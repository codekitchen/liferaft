package liferaft

import (
	"fmt"
	"math/rand"
	"slices"
)

// in-memory cluster, for testing
type InMemoryCluster struct {
	Nodemap map[NodeID]*memnode
	Nodes   []NodeID
	r       *rand.Rand

	// the delay frames for each link
	networkState map[networkLink]int
}

type memnode struct {
	Raft Raft
}

type memEvent struct {
	ev Event
	to NodeID
}

type networkLink struct {
	from, to NodeID
}

func NewInMemoryCluster(numNodes int, seed int64) *InMemoryCluster {
	cluster := &InMemoryCluster{
		Nodemap: make(map[NodeID]*memnode),
		Nodes:   make([]NodeID, numNodes),
		r:       rand.New(rand.NewSource(seed)),

		networkState: make(map[networkLink]int),
	}
	for n := range cluster.Nodes {
		cluster.Nodes[n] = NodeID(fmt.Sprintf("%d", n+1))
	}
	for _, id := range cluster.Nodes {
		cluster.Nodemap[id] = &memnode{
			Raft: *NewRaft(&RaftConfig{
				ID:      id,
				Client:  nil,
				Cluster: cluster.Nodes,
			}),
		}
		for _, other := range cluster.Nodes {
			cluster.networkState[networkLink{from: id, to: other}] = 0
		}
	}
	return cluster
}

func (c *InMemoryCluster) RunForTicks(ticks uint, afterTick func()) {
	var next []memEvent
	cmdIdx := 0

	for range ticks {
		cur := next
		next = nil

		// update network state
		for k := range c.networkState {
			if c.networkState[k] > 0 {
				c.networkState[k]--
			}
		}

		// set up initial ticks
		for _, n := range c.Nodes {
			cur = append(cur, memEvent{ev: &Tick{}, to: n})
		}

		badNodeIdx := c.r.Int() % 1_000
		if badNodeIdx < len(c.Nodes) {
			badNode := c.Nodes[badNodeIdx]
			badTime := c.r.Intn(1000)
			// uhoh, this node just got bad networking
			for k := range c.networkState {
				if k.from == badNode || k.to == badNode {
					c.networkState[k] = badTime
				}
			}
		}

		if c.r.Intn(50) == 0 {
			// do an apply to the leader
			cmdIdx++
			cmd := Apply{
				cmd: []byte(fmt.Sprintf("cmd:%d", cmdIdx)),
			}
			for _, n := range c.Nodes {
				if c.Nodemap[n].Raft.role == Leader {
					cur = append(cur, memEvent{ev: &cmd, to: n})
				}
			}
		}

		// TODO: this may loop forever if there's a bug that makes
		// the cluster infinitely chatty
		for len(cur) > 0 {
			// pick a deterministically random event
			idx := c.r.Intn(len(cur))
			ev := cur[idx]
			cur = slices.Delete(cur, idx, idx+1)

			if msg, ok := ev.ev.(*Message); ok {
				// if the message is from a node that's badly networked, delay it
				if c.networkState[networkLink{from: msg.From, to: msg.To}] > 0 {
					next = append(next, ev)
					continue
				}
			}

			updates := c.Nodemap[ev.to].Raft.HandleEvent(ev.ev)
			for _, msg := range updates.Outgoing {
				cur = append(cur, memEvent{ev: msg, to: msg.To})
			}
		}

		if afterTick != nil {
			afterTick()
		}
	}
}

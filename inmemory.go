package liferaft

import (
	"fmt"
	"math/rand"
	"slices"
)

type memnode struct {
	raft Raft
}

type memEvent struct {
	ev Event
	to NodeID
}

// in-memory cluster, for testing
type InMemoryCluster struct {
	nodemap map[NodeID]*memnode
	nodes   []NodeID
	r       *rand.Rand
}

func NewInMemoryCluster(numNodes int, seed int64) *InMemoryCluster {
	cluster := &InMemoryCluster{
		nodemap: make(map[NodeID]*memnode),
		nodes:   make([]NodeID, numNodes),
		r:       rand.New(rand.NewSource(seed)),
	}
	for n := range cluster.nodes {
		cluster.nodes[n] = NodeID(fmt.Sprintf("%d", n+1))
	}
	for n := range numNodes {
		id := cluster.nodes[n]
		cluster.nodemap[id] = &memnode{
			raft: *NewRaft(&RaftConfig{
				ID:      id,
				Client:  nil,
				Cluster: cluster.nodes,
			}),
		}
	}
	return cluster
}

func (c *InMemoryCluster) RunForTicks(ticks uint) {
	for range ticks {
		var q []memEvent
		// set up initial ticks
		for _, n := range c.nodes {
			q = append(q, memEvent{ev: &Tick{}, to: c.nodemap[n].raft.id})
		}
		// TODO: this may loop forever if there's a bug that makes
		// the cluster infinitely chatty
		for len(q) > 0 {
			idx := c.r.Intn(len(q))
			ev := q[idx]
			q = slices.Delete(q, idx, idx+1)
			updates := c.nodemap[ev.to].raft.HandleEvent(ev.ev)
			for _, msg := range updates.Outgoing {
				q = append(q, memEvent{ev: msg, to: msg.To})
			}
		}
	}
}

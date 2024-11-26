package liferaft

import (
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
)

type result struct {
	res any
	err error
}
type resultChan chan result

// For ephemeral nodes, the address is the ID.
// This makes it unsafe to restart a server once it has stopped, unless you restart the whole cluster.
type EphemeralRPCNode struct {
	mu sync.Mutex

	raft     Raft
	rpc      RaftRPC
	stop     chan struct{}
	incoming chan *Message
	outgoing chan *Message
	applies  chan *Apply
	client   Client

	waitingApplies map[string]resultChan
}

type RaftRPC interface {
	Run(incoming chan<- *Message, outgoing <-chan *Message)
}

func StartEphemeralNode(client Client, selfAddr string, otherAddrs []string) *EphemeralRPCNode {
	id := NodeID(selfAddr)
	cluster := []NodeID{id}
	for _, addr := range otherAddrs {
		cluster = append(cluster, NodeID(addr))
	}

	n := &EphemeralRPCNode{
		client: client,
		raft: *NewRaft(&RaftConfig{
			ID:                  id,
			Cluster:             cluster,
			ElectionTimeoutTick: uint(8 + rand.Intn(6)),
		}),
		rpc:            NewGoRPC(selfAddr, otherAddrs),
		stop:           make(chan struct{}),
		incoming:       make(chan *Message, 100),
		outgoing:       make(chan *Message, 100),
		applies:        make(chan *Apply),
		waitingApplies: make(map[string]resultChan),
	}

	go n.rpc.Run(n.incoming, n.outgoing)
	go n.run()
	return n
}

func (n *EphemeralRPCNode) Apply(cmd []byte) (any, error) {
	waiter := make(resultChan, 1)
	clientID := uuid.New().String()
	n.mu.Lock()
	n.waitingApplies[clientID] = waiter
	n.mu.Unlock()
	apply := &Apply{
		Cmd:      cmd,
		ClientID: clientID,
	}
	n.applies <- apply
	// TODO: need timeout here
	result := <-waiter
	close(waiter)
	n.mu.Lock()
	delete(n.waitingApplies, clientID)
	n.mu.Unlock()
	return result.res, result.err
}

func (n *EphemeralRPCNode) run() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		var event Event
		select {
		case <-ticker.C:
			event = &Tick{}
		case msg := <-n.incoming:
			event = msg
		case apply := <-n.applies:
			event = apply
		case <-n.stop:
			return
		}

		updates := n.raft.HandleEvent(event)
		// intentionally ignoring updates.Persist, cuz ephemeral
		for _, a := range updates.Apply {
			res, err := n.client.Apply(a.Cmd)
			n.mu.Lock()
			waiter, ok := n.waitingApplies[a.ClientID]
			delete(n.waitingApplies, a.ClientID)
			n.mu.Unlock()
			if ok {
				waiter <- result{res, err}
			}
		}
		for _, msg := range updates.Outgoing {
			n.outgoing <- msg
		}
	}
}

func (n *EphemeralRPCNode) Stop() {
	close(n.stop)
	close(n.outgoing)
}

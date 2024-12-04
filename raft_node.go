package liferaft

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrApplyTimeout = fmt.Errorf("apply timed out")

type RaftNode struct {
	mu sync.Mutex

	raft     Raft
	rpc      RaftRPC
	persist  RaftPersistence
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

type RaftPersistence interface {
	Persist(state *PersistentState) error
	Restore() (*PersistentState, error)
}

func StartRaftNode(
	client Client,
	rpc RaftRPC,
	persist RaftPersistence,
	config *RaftConfig,
) *RaftNode {
	n := &RaftNode{
		client:         client,
		raft:           *NewRaft(config),
		rpc:            rpc,
		persist:        persist,
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

func (n *RaftNode) Apply(cmd []byte) (any, error) {
	waiter := make(resultChan, 1)
	clientID := uuid.New().String()
	n.mu.Lock()
	n.waitingApplies[clientID] = waiter
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.waitingApplies, clientID)
		n.mu.Unlock()
	}()
	apply := &Apply{
		Cmd:      cmd,
		ClientID: clientID,
	}
	n.applies <- apply
	select {
	case result := <-waiter:
		return result.res, result.err
	case <-time.After(time.Second):
		return nil, ErrApplyTimeout
	}
}

func (n *RaftNode) Stop() {
	close(n.stop)
	close(n.outgoing)
}

type result struct {
	res any
	err error
}
type resultChan chan result

func (n *RaftNode) run() {
	ticker := time.NewTicker(50 * time.Millisecond)
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
		if updates.Persist != nil {
			err := n.persist.Persist(updates.Persist)
			// TODO: deal with this error once real persistence is implemented
			if err != nil {
				panic(err)
			}
		}

		for _, a := range updates.Apply {
			err := a.Err
			var res any
			if err == nil {
				res, err = n.client.Apply(a.Cmd)
			}
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

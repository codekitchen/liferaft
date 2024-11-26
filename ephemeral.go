package liferaft

import (
	"encoding/gob"
	"log"
	"math/rand"
	"net"
	"net/rpc"
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
	stop     chan struct{}
	incoming chan *Message
	applies  chan *Apply
	nodes    map[NodeID]*node
	client   Client

	waitingApplies map[string]resultChan
}

type node struct {
	address   string
	rpcClient *rpc.Client
}

func StartEphemeralNode(client Client, selfAddr string, otherAddrs []string) *EphemeralRPCNode {
	GobInit()

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
		nodes:          make(map[NodeID]*node),
		stop:           make(chan struct{}),
		incoming:       make(chan *Message, 100),
		applies:        make(chan *Apply),
		waitingApplies: make(map[string]resultChan),
	}
	for _, addr := range otherAddrs {
		n.nodes[NodeID(addr)] = &node{address: addr}
	}

	go n.runRPC(selfAddr)
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
			if n.raft.role != Leader {
				n.relayApplyToLeader(apply)
				continue
			}
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
			n.sendRPC(msg)
		}
	}
}

func (n *EphemeralRPCNode) sendRPC(msg *Message) {
	client := n.nodes[msg.To]
	if client == nil {
		log.Fatal("attempt to send message to unknown node", msg)
	}
	var err error

	if client.rpcClient == nil {
		client.rpcClient, err = rpc.Dial("tcp", client.address)
	}
	if err == nil {
		err = client.rpcClient.Call("Ephemeral.Receive", msg, nil)
	}

	if err != nil {
		// slog.Error("failed to send message to node", "msg", msg, "error", err)
		if client.rpcClient != nil {
			client.rpcClient.Close()
			client.rpcClient = nil
		}
	}
}

func (n *EphemeralRPCNode) relayApplyToLeader(apply *Apply) {
	to := n.raft.leaderID
	if to == NoNode {
		// TODO: get this error back to the client
		return
	}
	client := n.nodes[to]
	if client == nil {
		log.Fatal("invalid leader node id")
	}
	var err error

	if client.rpcClient == nil {
		client.rpcClient, err = rpc.Dial("tcp", client.address)
	}
	if err == nil {
		err = client.rpcClient.Call("Ephemeral.RelayApply", apply, nil)
	}

	if err != nil {
		// slog.Error("failed to send message to node", "msg", msg, "error", err)
		if client.rpcClient != nil {
			client.rpcClient.Close()
			client.rpcClient = nil
		}
	}
}

// TODO: rsp is unused here, how to model that?
func (n *EphemeralRPCNode) Receive(msg *Message, rsp *Message) error {
	n.incoming <- msg
	return nil
}

func (n *EphemeralRPCNode) RelayApply(apply *Apply, rsp *Apply) error {
	n.applies <- apply
	return nil
}

func (n *EphemeralRPCNode) runRPC(listenAddr string) {
	server := rpc.NewServer()
	server.RegisterName("Ephemeral", n)
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		panic(err)
	}
	server.Accept(l)
}

func (n *EphemeralRPCNode) Stop() {
	close(n.stop)
}

func GobInit() {
	gob.Register((*RequestVote)(nil))
	gob.Register((*RequestVoteResponse)(nil))
	gob.Register((*AppendEntries)(nil))
	gob.Register((*AppendEntriesResponse)(nil))
}

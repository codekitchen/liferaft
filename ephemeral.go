package liferaft

import (
	"encoding/gob"
	"log"
	"math/rand"
	"net"
	"net/rpc"
	"time"
)

// For ephemeral nodes, the address is the ID.
// This makes it unsafe to restart a server once it has stopped, unless you restart the whole cluster.
type EphemeralRPCNode struct {
	raft     Raft
	stop     chan struct{}
	incoming chan *Message
	nodes    map[NodeID]*node
}

type node struct {
	address   string
	rpcClient *rpc.Client
}

func StartEphemeralNode(client Client, selfAddr string, otherAddrs []string) *EphemeralRPCNode {
	gobInit()

	id := NodeID(selfAddr)
	cluster := []NodeID{id}
	for _, addr := range otherAddrs {
		cluster = append(cluster, NodeID(addr))
	}

	n := &EphemeralRPCNode{
		raft: *NewRaft(&RaftConfig{
			ID:                  id,
			Client:              client,
			Cluster:             cluster,
			ElectionTimeoutTick: uint(8 + rand.Intn(6)),
		}),
		nodes:    make(map[NodeID]*node),
		stop:     make(chan struct{}),
		incoming: make(chan *Message, 100),
	}
	for _, addr := range otherAddrs {
		n.nodes[NodeID(addr)] = &node{address: addr}
	}

	go n.runRPC(selfAddr)
	go n.run()
	return n
}

func (n *EphemeralRPCNode) Apply(cmd []byte) ([]byte, error) {
	return n.raft.Apply(cmd)
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
		case <-n.stop:
			return
		}

		updates := n.raft.HandleEvent(event)
		// intentionally ignoring updates.Persist, cuz ephemeral
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
		// slog.Warn("failed to send message to node", "msg", msg, "error", err)
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

func gobInit() {
	gob.Register(&RequestVote{})
	gob.Register(&RequestVoteResponse{})
	gob.Register(&AppendEntries{})
	gob.Register(&AppendEntriesResponse{})
}

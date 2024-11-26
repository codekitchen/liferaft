package liferaft

import (
	"encoding/gob"
	"log"
	"net"
	"net/rpc"
)

type GoRPC struct {
	selfAddr string
	nodes    map[NodeID]*goRPCNode
	incoming chan<- *Message
}

type goRPCNode struct {
	address   string
	rpcClient *rpc.Client
}

func NewGoRPC(selfAddr string, allAddrs []string) *GoRPC {
	rpc := &GoRPC{
		selfAddr: selfAddr,
		nodes:    make(map[string]*goRPCNode),
	}
	for _, addr := range allAddrs {
		rpc.nodes[NodeID(addr)] = &goRPCNode{address: addr}
	}
	return rpc
}

func (r *GoRPC) Run(incoming chan<- *Message, outgoing <-chan *Message) {
	GobInit()
	r.incoming = incoming
	server := rpc.NewServer()
	server.RegisterName("Ephemeral", r)
	l, err := net.Listen("tcp", r.selfAddr)
	if err != nil {
		panic(err)
	}
	go server.Accept(l)
	defer l.Close()

	for msg := range outgoing {
		r.sendRPC(msg)
	}
}

func (r *GoRPC) sendRPC(msg *Message) {
	client := r.nodes[msg.To]
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
		if client.rpcClient != nil {
			client.rpcClient.Close()
			client.rpcClient = nil
		}
	}
}

// TODO: rsp is unused here, how to model that?
func (r *GoRPC) Receive(msg *Message, rsp *Message) error {
	r.incoming <- msg
	return nil
}

func GobInit() {
	gob.Register((*RequestVote)(nil))
	gob.Register((*RequestVoteResponse)(nil))
	gob.Register((*AppendEntries)(nil))
	gob.Register((*AppendEntriesResponse)(nil))
	gob.Register((*RelayApply)(nil))
}

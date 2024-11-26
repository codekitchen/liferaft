package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"os"
	"sync/atomic"

	"github.com/codekitchen/liferaft"
)

type MaelstromRPC struct {
	outgoing     chan *Message
	raftIncoming chan<- *liferaft.Message
	nodeID       string
	msgID        atomic.Int32
}

// Run implements liferaft.RaftRPC.
func (m *MaelstromRPC) Run(incoming chan<- *liferaft.Message, outgoing <-chan *liferaft.Message) {
	m.raftIncoming = incoming
	for msg := range outgoing {
		m.sendRaft(msg)
	}
}

func NewMaelstromRPC() *MaelstromRPC {
	return &MaelstromRPC{
		outgoing: make(chan *Message),
	}
}

func (m *MaelstromRPC) RunMaelstrom(incoming chan<- *Message) {
	liferaft.GobInit()
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	go func() {
		for {
			msg := <-m.outgoing
			perr(enc.Encode(msg))
		}
	}()

	for {
		if !scanner.Scan() {
			break
		}
		line := scanner.Bytes()
		var msg Message
		unmarshal(line, &msg)

		if msg.Body().Type == "raft" {
			var body BodyRaft
			msg.ParseBody(&body)
			var rmsg *liferaft.Message
			dec := gob.NewDecoder(bytes.NewBuffer(body.RMsg))
			perr(dec.Decode(&rmsg))
			m.raftIncoming <- rmsg
			continue
		}
		if msg.Body().Type == "init" {
			var body BodyInit
			msg.ParseBody(&body)
			m.nodeID = body.NodeID
		}
		incoming <- &msg
	}
}

func (m *MaelstromRPC) reply(req *Message, msgType string, resBody any) {
	m.send(msgType, req.Src, &req.Body().MsgID, resBody)
}

// thread safe, once m.nodeID is set
func (m *MaelstromRPC) send(msgType, dest string, inReplyTo *int, resBody any) {
	if m.nodeID == "" {
		panic("don't have nodeID yet")
	}
	resM := &Message{
		Src:  m.nodeID,
		Dest: dest,
	}
	bytes := marshal(resBody)
	bodyMap := make(map[string]any)
	unmarshal(bytes, &bodyMap)
	bodyMap["type"] = msgType
	bodyMap["msg_id"] = m.msgID.Add(1)
	bodyMap["in_reply_to"] = inReplyTo
	bytes = marshal(&bodyMap)
	resM.RawBody = bytes
	m.outgoing <- resM
}

func (m *MaelstromRPC) sendRaft(rmsg *liferaft.Message) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	enc.Encode(rmsg)
	body := &BodyRaft{
		RMsg: buf.Bytes(),
	}
	m.send("raft", rmsg.To, nil, body)
}

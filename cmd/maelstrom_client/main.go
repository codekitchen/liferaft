package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"math/rand"

	"github.com/codekitchen/liferaft"
	kv "github.com/codekitchen/liferaft/kv"
)

type Message struct {
	Src  string          `json:"src"`
	Dest string          `json:"dest"`
	Body json.RawMessage `json:"body"`
}

type Body struct {
	Type      string `json:"type"`
	MsgID     int    `json:"msg_id"`
	InReplyTo int    `json:"in_reply_to,omitempty"`
}

type BodyInit struct {
	NodeID  string   `json:"node_id"`
	NodeIDs []string `json:"node_ids"`
}

type BodyEcho struct {
	Echo string `json:"echo"`
}

type BodyKV struct {
	Key   any `json:"key,omitempty"`
	Value any `json:"value"`
	From  any `json:"from,omitempty"`
	To    any `json:"to,omitempty"`
}

type BodyError struct {
	Code int `json:"code"`
}

type BodyRaft struct {
	RMsg []byte
}

type ClientRequest struct {
	req   *Message
	apply *liferaft.Apply
}

type Server struct {
	nodeID  string
	nodeIDs []string
	msgID   int
	writes  chan *Message
	store   *kv.Store
	reqMap  map[string]*Message
}

func (s *Server) runRaft(incoming <-chan liferaft.Event, requests <-chan *ClientRequest) {
	raft := liferaft.NewRaft(&liferaft.RaftConfig{
		ID:                  s.nodeID,
		Cluster:             s.nodeIDs,
		ElectionTimeoutTick: uint(8 + rand.Intn(6)),
	})
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		var event liferaft.Event
		select {
		case <-ticker.C:
			event = &liferaft.Tick{}
		case msg := <-incoming:
			event = msg
		case req := <-requests:
			var body Body
			unmarshal(req.req.Body, &body)
			req.apply.ClientID = req.req.Src + ":" + itoa(body.MsgID)
			if raft.IsLeader() {
				s.reqMap[req.apply.ClientID] = req.req
				event = req.apply
			} else if raft.LeaderID() != liferaft.NoNode {
				s.reqMap[req.apply.ClientID] = req.req
				// proxy to the leader if possible
				s.sendRaftApply(raft.LeaderID(), req)
				continue
			} else {
				s.reply(req.req, "error", &BodyError{Code: 11})
				continue
			}
		}

		updates := raft.HandleEvent(event)
		for _, a := range updates.Apply {
			res, err := s.store.Apply(a.Cmd)
			req, ok := s.reqMap[a.ClientID]
			if ok {
				s.replyToRequest(req, res, err)
				delete(s.reqMap, a.ClientID)
			}
		}
		for _, msg := range updates.Outgoing {
			s.sendRaft(msg)
		}
	}
}

const (
	MaelstromErrorAbort              = 14
	MaelstromErrorKeyDoesNotExist    = 20
	MaelstromErrorPreconditionFailed = 22
)

func (s *Server) replyToRequest(req *Message, res any, err error) {
	if err != nil {
		code := MaelstromErrorAbort
		if err == kv.KeyNotFoundError {
			code = MaelstromErrorKeyDoesNotExist
		} else if err == kv.CASMismatchError {
			code = MaelstromErrorPreconditionFailed
		}
		s.reply(req, "error", &BodyError{Code: code})
		return
	}
	var body Body
	unmarshal(req.Body, &body)
	switch body.Type {
	case "read":
		s.reply(req, "read_ok", &BodyKV{Value: res})
	case "write":
		s.reply(req, "write_ok", &Body{})
	case "cas":
		s.reply(req, "cas_ok", &Body{})
	default:
		panic(fmt.Sprintf("unknown req type `%s`", body.Type))
	}
}

func (s *Server) Run(read chan *Message) {
	incoming := make(chan liferaft.Event)
	requests := make(chan *ClientRequest)

	for msg := range read {
		var body Body
		unmarshal(msg.Body, &body)
		switch body.Type {
		case "init":
			var body BodyInit
			unmarshal(msg.Body, &body)
			s.nodeID = body.NodeID
			s.nodeIDs = body.NodeIDs
			go s.runRaft(incoming, requests)
			s.reply(msg, "init_ok", &Body{})
		case "echo":
			var body BodyEcho
			unmarshal(msg.Body, &body)
			s.reply(msg, "echo_ok", &BodyEcho{Echo: body.Echo})
		case "read":
			var kvbody BodyKV
			unmarshal(msg.Body, &kvbody)
			apply := &liferaft.Apply{
				Cmd: kv.Command{Op: "get", Key: anyToString(kvbody.Key)}.Marshal(),
			}
			requests <- &ClientRequest{
				req:   msg,
				apply: apply,
			}
		case "write":
			var kvbody BodyKV
			unmarshal(msg.Body, &kvbody)
			apply := &liferaft.Apply{
				Cmd: kv.Command{Op: "set", Key: anyToString(kvbody.Key), Value: kvbody.Value}.Marshal(),
			}
			requests <- &ClientRequest{
				req:   msg,
				apply: apply,
			}
		case "cas":
			var kvbody BodyKV
			unmarshal(msg.Body, &kvbody)
			apply := &liferaft.Apply{
				Cmd: kv.Command{Op: "cas", Key: anyToString(kvbody.Key), From: kvbody.From, To: kvbody.To}.Marshal(),
			}
			requests <- &ClientRequest{
				req:   msg,
				apply: apply,
			}
		case "raft":
			var body BodyRaft
			unmarshal(msg.Body, &body)
			var rmsg *liferaft.Message
			dec := gob.NewDecoder(bytes.NewBuffer(body.RMsg))
			err := dec.Decode(&rmsg)
			if err != nil {
				panic(err)
			}
			incoming <- rmsg
		case "raft_apply":
			var body BodyRaft
			unmarshal(msg.Body, &body)
			var apply *liferaft.Apply
			dec := gob.NewDecoder(bytes.NewBuffer(body.RMsg))
			err := dec.Decode(&apply)
			if err != nil {
				panic(err)
			}
			incoming <- apply
		}
	}
}

func itoa(i int) string {
	return strconv.FormatInt(int64(i), 10)
}

func (s *Server) reply(req *Message, msgType string, resBody any) {
	var reqBody Body
	unmarshal(req.Body, &reqBody)
	s.send(msgType, req.Src, &reqBody.MsgID, resBody)
}

func (s *Server) send(msgType, dest string, inReplyTo *int, resBody any) {
	resM := &Message{
		Src:  s.nodeID,
		Dest: dest,
	}
	s.msgID++
	bytes := marshal(resBody)
	bodyMap := make(map[string]any)
	unmarshal(bytes, &bodyMap)
	bodyMap["type"] = msgType
	bodyMap["msg_id"] = s.msgID
	bodyMap["in_reply_to"] = inReplyTo
	bytes = marshal(&bodyMap)
	resM.Body = bytes
	s.writes <- resM
}

func (s *Server) sendRaft(rmsg *liferaft.Message) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	enc.Encode(rmsg)
	body := &BodyRaft{
		RMsg: buf.Bytes(),
	}
	s.send("raft", rmsg.To, nil, body)
}

func (s *Server) sendRaftApply(dest string, req *ClientRequest) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	enc.Encode(req.apply)
	body := &BodyRaft{
		RMsg: buf.Bytes(),
	}
	s.send("raft_apply", dest, nil, body)
}

func main() {
	liferaft.GobInit()
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	errenc := json.NewEncoder(os.Stderr)
	fromServer := make(chan *Message, 1)
	toServer := make(chan *Message, 1)

	server := &Server{
		writes: fromServer,
		store:  kv.NewKVStore(),
		reqMap: make(map[string]*Message),
	}
	go server.Run(toServer)

	go func() {
		for {
			msg := <-fromServer
			err := enc.Encode(msg)
			if err != nil {
				panic(err)
			}
			os.Stderr.WriteString("send: ")
			errenc.Encode(msg)
		}
	}()

	for {
		if !scanner.Scan() {
			break
		}
		line := scanner.Bytes()
		var msg Message
		unmarshal(line, &msg)

		os.Stderr.WriteString("recv: ")
		errenc.Encode(msg)
		toServer <- &msg
	}
}

func unmarshal(data []byte, v any) {
	perr(json.Unmarshal(data, v))
}

func marshal(v any) []byte {
	bytes, err := json.Marshal(v)
	perr(err)
	return bytes
}

func perr(err error) {
	if err != nil {
		panic(err)
	}
}

func anyToString(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case int:
		return itoa(c)
	case float64:
		return itoa(int(c))
	default:
		panic(fmt.Sprintf("bad type %#v", v))
	}
}

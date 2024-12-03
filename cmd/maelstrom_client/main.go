package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"math/rand"

	"github.com/codekitchen/liferaft"
	kv "github.com/codekitchen/liferaft/kv"
)

const (
	MaelstromErrorTimeout                = 0
	MaelstromErrorTemporarilyUnavailable = 11
	MaelstromErrorAbort                  = 14
	MaelstromErrorKeyDoesNotExist        = 20
	MaelstromErrorPreconditionFailed     = 22
)

func itoa(i int) string {
	return strconv.FormatInt(int64(i), 10)
}

func errorBodyFor(err error) *BodyError {
	code := MaelstromErrorAbort
	switch err {
	case kv.ErrKeyNotFound:
		code = MaelstromErrorKeyDoesNotExist
	case kv.ErrCASMismatch:
		code = MaelstromErrorPreconditionFailed
	case liferaft.ErrApplyTimeout:
		code = MaelstromErrorTimeout
		code = MaelstromErrorTemporarilyUnavailable
	}
	return &BodyError{Code: code}
}

func main() {
	store := kv.NewKVStore()
	rpc := NewMaelstromRPC()
	persist := &liferaft.EphemeralPersistence{}
	incoming := make(chan *Message)
	go rpc.RunMaelstrom(incoming)

	initMsg := <-incoming
	if initMsg.Body().Type != "init" {
		panic("expected init message")
	}
	var init BodyInit
	initMsg.ParseBody(&init)

	node := liferaft.StartRaftNode(store, rpc, persist, &liferaft.RaftConfig{
		ID:                  init.NodeID,
		Cluster:             init.NodeIDs,
		ElectionTimeoutTick: uint(8 + rand.Intn(6)),
	})
	rpc.reply(initMsg, "init_ok", &Body{})

	for msg := range incoming {
		switch msg.Body().Type {
		case "echo":
			var body BodyEcho
			msg.ParseBody(&body)
			rpc.reply(msg, "echo_ok", &BodyEcho{Echo: body.Echo})
		case "read":
			go func() {
				var kvbody BodyKV
				msg.ParseBody(&kvbody)
				res, err := node.Apply(kv.Command{Op: "get", Key: anyToString(kvbody.Key)}.Marshal())
				if err != nil {
					rpc.reply(msg, "error", errorBodyFor(err))
				} else {
					rpc.reply(msg, "read_ok", &BodyKV{Value: res})
				}
			}()
		case "write":
			go func() {
				var kvbody BodyKV
				msg.ParseBody(&kvbody)
				_, err := node.Apply(kv.Command{Op: "set", Key: anyToString(kvbody.Key), Value: kvbody.Value}.Marshal())
				if err != nil {
					rpc.reply(msg, "error", errorBodyFor(err))
				} else {
					rpc.reply(msg, "write_ok", &Body{})
				}
			}()
		case "cas":
			go func() {
				var kvbody BodyKV
				msg.ParseBody(&kvbody)
				_, err := node.Apply(kv.Command{Op: "cas", Key: anyToString(kvbody.Key), From: kvbody.From, To: kvbody.To}.Marshal())
				if err != nil {
					rpc.reply(msg, "error", errorBodyFor(err))
				} else {
					rpc.reply(msg, "cas_ok", &Body{})
				}
			}()
		default:
			panic(fmt.Sprint("unknown message type", msg.Body().Type))
		}
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
	case nil:
		panic("type is nil")
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

// {"src": "c1", "dest": "n1", "body": {"msg_id": 1, "type": "init", "node_id": "n1", "node_ids": ["n1","n2","n3"]}}
// {"src": "c1", "dest": "n1", "body": {"msg_id": 2, "type": "echo", "echo": "hello there"}}
// {"src": "c1", "dest": "n1", "body": {"msg_id": 3, "type": "read", "key": 3}}

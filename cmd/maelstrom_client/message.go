package main

import "encoding/json"

type Message struct {
	Src     string          `json:"src"`
	Dest    string          `json:"dest"`
	RawBody json.RawMessage `json:"body"`
}

func (m *Message) Body() *Body {
	var body Body
	unmarshal(m.RawBody, &body)
	return &body
}

// ParseBody parses the body into one of the subtypes.
func (m *Message) ParseBody(body any) {
	unmarshal(m.RawBody, body)
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

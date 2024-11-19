package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/codekitchen/liferaft"
)

type store struct {
	mu   sync.RWMutex
	data map[string]string
}

func (s *store) Get(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[key]
}

func (s *store) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

type command struct {
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (c command) Marshal() []byte {
	res, _ := json.Marshal(c)
	return res
}

func (s *store) Apply(cmd []byte) ([]byte, error) {
	var c command
	err := json.Unmarshal(cmd, &c)
	if err != nil {
		panic(err)
	}
	switch c.Op {
	case "set":
		s.Set(c.Key, c.Value)
		return nil, nil
	case "get":
		val, ok := s.data[c.Key]
		if !ok {
			return nil, fmt.Errorf("key not found")
		}
		return []byte(val), nil
	}
	return nil, fmt.Errorf("unknown op %s", c.Op)
}

func main() {
	selfAddr := flag.String("self", "", "address of this node")
	othersStr := flag.String("others", "", "addresses of other nodes")
	flag.Parse()
	others := strings.Split(*othersStr, ",")
	if *selfAddr == "" || others[0] == "," {
		flag.Usage()
		os.Exit(1)
	}

	store := store{data: make(map[string]string)}
	node := liferaft.StartEphemeralNode(&store, *selfAddr, others)
	defer node.Stop()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("$ ")
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		parts := strings.SplitN(line, " ", 3)
		switch parts[0] {
		case "set":
			_, err := node.Apply(command{Op: "set", Key: parts[1], Value: parts[2]}.Marshal())
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println("ok")
			}
		case "get":
			res, err := node.Apply(command{Op: "get", Key: parts[1]}.Marshal())
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println(string(res))
			}
		}
	}
}

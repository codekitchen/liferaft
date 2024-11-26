package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/codekitchen/liferaft"
	kv "github.com/codekitchen/liferaft/kv"
)

func main() {
	selfAddr := flag.String("self", "", "address of this node")
	othersStr := flag.String("others", "", "addresses of other nodes")
	flag.Parse()
	others := strings.Split(*othersStr, ",")
	if *selfAddr == "" || others[0] == "," {
		flag.Usage()
		os.Exit(1)
	}

	store := kv.NewKVStore()
	node := liferaft.StartEphemeralNode(store, *selfAddr, others)
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
			_, err := node.Apply(kv.Command{Op: "set", Key: parts[1], Value: parts[2]}.Marshal())
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println("ok")
			}
		case "get":
			res, err := node.Apply(kv.Command{Op: "get", Key: parts[1]}.Marshal())
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println(res)
			}
		}
	}
}

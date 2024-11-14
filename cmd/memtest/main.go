package main

import "github.com/codekitchen/liferaft"

func main() {
	cluster := liferaft.NewInMemoryCluster(3, 1234)
	cluster.RunForTicks(50_000)
}

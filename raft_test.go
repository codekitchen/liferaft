package liferaft

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"
)

// TEST_SEED=984927255
// add event tracing and output the trace on failure

var exploreTest = flag.String("exploreTest", "", "run an exploration test")

func TestElection(t *testing.T) {
	if *exploreTest != t.Name() {
		t.SkipNow()
	}
	if os.Getenv("TEST_SEED") != "" {
		seed, _ := strconv.ParseInt(os.Getenv("TEST_SEED"), 10, 64)
		runOne(t, seed)
		return
	}
	execs := 0
	starttime := time.Now()
	lastMessage := time.Now()
	for {
		if time.Since(lastMessage) > time.Second*3 {
			lastMessage = time.Now()
			fmt.Fprintf(os.Stderr, "=== %s: elasped: %s, execs: %d (%d/sec)\n", t.Name(), time.Since(starttime).Truncate(time.Second), execs, execs/int(time.Since(starttime).Seconds()+1))
		}
		seed := int64(rand.Int31())
		res := runOne(t, seed)
		if !res {
			break
		}
		execs++
	}
}

func runOne(t *testing.T, seed int64) bool {
	success := t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
		defer func() {
			if t.Failed() {
				t.Logf("TEST_SEED=%d", seed)
			}
		}()
		cluster := NewInMemoryCluster(3, seed)
		cluster.RunForTicks(1_000)
		leaderInvariant(t, cluster)
	})
	return success
}

// There should not be more than one leader for the same term at the same time.
func leaderInvariant(t *testing.T, cluster *InMemoryCluster) {
	t.Helper()
	leaderCounts := make(map[Term]int)
	for _, n := range cluster.Nodemap {
		if n.Raft.role == Leader {
			leaderCounts[n.Raft.currentTerm]++
		}
	}
	for term, count := range leaderCounts {
		if count > 1 {
			t.Fatalf("expected at most 1 leader for term %d, got %d", term, count)
		}
	}
}

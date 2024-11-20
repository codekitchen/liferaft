package liferaft

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"
)

var exploreTest = flag.String("exploreTest", "", "run an exploration test")

func TestNetworkBadness(t *testing.T) {
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
		res := t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			runOne(t, seed)
		})
		if !res {
			break
		}
		execs++
	}
}

func runOne(t *testing.T, seed int64) {
	defer func() {
		if t.Failed() {
			t.Logf("TEST_SEED=%d", seed)
		}
	}()
	cluster := NewInMemoryCluster(3, seed)
	cluster.RunForTicks(1_000, func() {
		leaderInvariant(t, cluster)
		logInvariant(t, cluster)
		electionSafetyInvariant(t, cluster)
	})
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

// Committed log entries should never conflict between servers.
func logInvariant(t *testing.T, cluster *InMemoryCluster) {
	for i, n1 := range cluster.Nodes {
		log1 := cluster.Nodemap[n1].Raft.CommittedLog()
		for _, n2 := range cluster.Nodes[i+1:] {
			log2 := cluster.Nodemap[n2].Raft.CommittedLog()
			checkLen := min(len(log1), len(log2))
			if !slices.EqualFunc(log1[:checkLen], log2[:checkLen], entryEq) {
				t.Fatalf("log conflict between %s and %s", n1, n2)
			}
		}
	}
}

// A leader always has the greatest index for its current term
func electionSafetyInvariant(t *testing.T, cluster *InMemoryCluster) {
	for _, n := range cluster.Nodemap {
		if n.Raft.role == Leader {
			term := n.Raft.currentTerm
			lidx := maxIndexForTerm(n.Raft.log, term)
			for _, n2 := range cluster.Nodemap {
				ridx := maxIndexForTerm(n2.Raft.log, term)
				if lidx < ridx {
					t.Fatalf("leader %s does not have greatest index for term %d", n.Raft.id, term)
				}
			}
		}
	}
}

func maxIndexForTerm(log []Entry, term Term) int {
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].Term == term {
			return i
		}
	}
	return 0
}

func entryEq(e1, e2 Entry) bool {
	return e1.Term == e2.Term && slices.Equal(e1.Cmd, e2.Cmd)
}

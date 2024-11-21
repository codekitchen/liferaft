package liferaft

import (
	"slices"
	"testing"
)

func allInvariants(t *testing.T, cluster *InMemoryCluster) {
	leaderInvariant(t, cluster)
	logInvariant(t, cluster)
	logPrefixInvariant(t, cluster)
	electionSafetyInvariant(t, cluster)
	quorumLogInvariant(t, cluster)
	moreUpToDateInvariant(t, cluster)
	leaderCompletenessInvariant(t, cluster)
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

// Every (index, term) pair determines a log prefix.
// In other words: if two servers have a log entry with the same term at the same index,
// all previous log entries match on both servers.
func logPrefixInvariant(t *testing.T, cluster *InMemoryCluster) {
	for i, n1 := range cluster.Nodes {
		log1 := cluster.Nodemap[n1].Raft.log
		for _, n2 := range cluster.Nodes[i+1:] {
			log2 := cluster.Nodemap[n2].Raft.log
			// find the latest entry with matching term in both logs
			for idx := min(len(log1), len(log2)) - 1; idx >= 0; idx-- {
				if log1[idx].Term == log2[idx].Term {
					if !slices.EqualFunc(log1[:idx], log2[:idx], entryEq) {
						t.Fatalf("log prefixes do not match between %s and %s at index %d", n1, n2, idx)
					}
					break
				}
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

// All committed entries are contained in the log of at least one server in every possible quorum
func quorumLogInvariant(t *testing.T, cluster *InMemoryCluster) {
	// I think this is equivalent to saying: all committed entries are contained in the log of a majority of servers.
	for _, n := range cluster.Nodes {
		committed := cluster.Nodemap[n].Raft.CommittedLog()
		count := 1
		for _, n2 := range cluster.Nodes {
			if n == n2 {
				continue
			}
			if logIsPrefix(committed, cluster.Nodemap[n2].Raft.log) {
				count++
			}
		}
		if count < quorumSize(len(cluster.Nodes)) {
			t.Fatalf("committed log for %s is not on majority of nodes", n)
		}
	}
}

// The "up-to-date" check performed by servers
// before issuing a vote implies that i receives
// a vote from j only if i has all of j's committed
// entries
func moreUpToDateInvariant(t *testing.T, cluster *InMemoryCluster) {
	for i, n1 := range cluster.Nodes {
		log1 := cluster.Nodemap[n1].Raft.log
		for _, n2 := range cluster.Nodes[i+1:] {
			log2 := cluster.Nodemap[n2].Raft.log
			if lastTerm(log1) > lastTerm(log2) || (lastTerm(log1) == lastTerm(log2) && len(log1) >= len(log2)) {
				if !logIsPrefix(cluster.Nodemap[n2].Raft.CommittedLog(), log1) {
					t.Fatalf("committed log on %s isn't prefix of %s", n2, n1)
				}
			}
		}
	}
}

// If a log entry is committed in a given term, then that
// entry will be present in the logs of the leaders
// for all higher-numbered terms
func leaderCompletenessInvariant(t *testing.T, cluster *InMemoryCluster) {
	for _, n := range cluster.Nodes {
		committed := cluster.Nodemap[n].Raft.CommittedLog()
		if len(committed) == 0 {
			continue
		}
		lastIdx := len(committed) - 1
		for _, n2 := range cluster.Nodes {
			r2 := cluster.Nodemap[n2].Raft
			if n == n2 || r2.role != Leader {
				continue
			}
			if r2.currentTerm > committed[lastIdx].Term {
				if !entryEq(r2.log[lastIdx], committed[lastIdx]) {
					t.Fatalf("committed log on %s does not match leader %s", n, n2)
				}
			}
		}
	}
}

func lastTerm(log []Entry) Term {
	if len(log) == 0 {
		return Term(0)
	}
	return log[len(log)-1].Term
}

func maxIndexForTerm(log []Entry, term Term) int {
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].Term == term {
			return i
		}
	}
	return 0
}

func logIsPrefix(log1, log2 []Entry) bool {
	return len(log1) <= len(log2) && slices.EqualFunc(log1, log2[:len(log1)], entryEq)
}

func entryEq(e1, e2 Entry) bool {
	return e1.Term == e2.Term && slices.Equal(e1.Cmd, e2.Cmd)
}

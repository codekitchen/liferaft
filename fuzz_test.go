package liferaft

import "testing"

// Attempt to fit the simulation testing into go's built-in fuzz tester.
// It works, but the fuzz tester is a bit restricting as it wasn't really
// designed for quite this use case. Abandoning for now.

// Something that really tripped me up until I figured out what was going on:
// when fuzzing through random exploration, after baseline is established,
// the fuzz framework is spawning subprocesses and not just running each fuzz attempt
// in a goroutine. This has consequences for global counters, attaching delve for debugging, etc.
func FuzzNetworkBadness(f *testing.F) {
	f.Add(int64(1467554846))
	f.Fuzz(func(t *testing.T, seed int64) {
		runOne(t, seed)
	})
}

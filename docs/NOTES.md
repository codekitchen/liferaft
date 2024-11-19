# Notes

## Potential Improvements and TODOs

- [ ] parallelize exploratory test runs
- [ ] output traces of events, and support them for re-running a test (possibly more stable alternative to rng seed)
- [ ] other types of badness: failing nodes, duplicate packets, etc
- [ ] how long to run each test for? 1000 steps is arbitrary
- [ ] run this raft code against [maelstrom](https://github.com/jepsen-io/maelstrom)
- [ ]

## Sources of Non-Determinism in Go

This list is a work-in-progress

- I/O
- parallelism
- scheduler
- select
- timers
- map iteration

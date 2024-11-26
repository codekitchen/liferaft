# Notes

## Potential Improvements and TODOs

- [ ] parallelize exploratory test runs
- [ ] priority queue of events schedule for specific times, instead of flat list (but compress time)
- [ ] output traces of events, and support them for re-running a test (possibly more stable alternative to rng seed)
- [x] other types of badness: failing nodes, duplicate packets, etc
- [x] how long to run each test for? 1000 steps is arbitrary
- [x] run this raft code against [maelstrom](https://github.com/jepsen-io/maelstrom)
- [ ] try using go fuzz testing with a form of [sometimes assertions](https://antithesis.com/docs/best_practices/sometimes_assertions/) to guide the fuzzer toward interesting states. need to think about this more and read how go fuzz testing directs itself toward uncovered code.

## Sources of Non-Determinism in Go

This list is a work-in-progress. See also [this blog post](https://blog.merovius.de/posts/2018-01-15-generating_entropy_without_imports_in_go/).

- I/O
- parallelism
- scheduler
- select
- timers
- map iteration

# LifeRaft

LifeRaft is a Go implementation of the [Raft consensus protocol](https://raft.github.io/). Raft is a protocol to maintain a consistent linearizable log replicated across a cluster of Raft nodes.

The implementation of Raft isn't really the goal here, though. Raft is being used as a convenient test case for exploring modern techniques for testing distributed systems (and locally concurrent systems, but the focus is on distributed).

My primary focus right now is on deterministic simulation of distributed systems.

## Current Focus

The core of this Raft implementation is a "pure" deterministic state machine. Given the same state and the same next input, the library will always return the same result. This deterministic approach is key to repeatable testing.

Of course, this precludes doing anything dirty like actually talking to network or disk, or even using timers in some ways. Those concerns are built as layers on top of the core Raft protocol.

This necessarily makes the code a little less idiomatic for Go -- for instance, even a core language feature like `select` is intentionally non-deterministic when more than one channel is ready, so we can't use `select` in the core Raft logic. More sources of non-determinism in Go are covered in [the notes](NOTES.md).

## Testing Framework

There is an in-memory cluster implementation that can be used for repeatable testing. I am beginning to build a "chaos framework" on top of this, allowing for things like introducing network delays between specific nodes in the cluster.

I've already seen success with these techniques, finding a bug in leader election when network responses are delayed to a later campaign. It took about 8 minutes running the (very un-optimized) randomized tests and checking the invariants to expose this bug, which otherwise could have very easily made it past other testing techniques and into "production".

But once a failing test case is found, we can re-run it at will! This is such a huge win. Often it's very challenging to come up with a reproducable test case for a bug like this, to the point where many times the fix is implemented without such a test case.

## Further Reading on Deterministic Simulation Techniques

- [Deterministic Simulation link collection](https://asatarin.github.io/testing-distributed-systems/#deterministic-simulation)
- [Testing Distributed Systems w/ Deterministic Simulation](https://www.youtube.com/watch?v=4fFDFbi3toc&feature=youtu.be)
- [Antithesis](https://antithesis.com) is an interesting, very general solution (deterministic VM!) from some of the same people behind FoundationDB. Closed source.
- The [Hermit project](https://github.com/facebookexperimental/hermit) from Facebook tries to accomplish the same thing in a different way, by hooking into syscalls. Development seems to be paused.
- There are a few interesting Rust libraries in this space.
  - [Shuttle](https://github.com/awslabs/shuttle) is an AWS library for randomized deterministic testing of Rust programs. Very cool that the core S3 storage node service is now written in Rust, and tested with Shuttle.
  - [Loom](https://github.com/tokio-rs/loom) is a Tokio project for exhaustive deterministic testing of Rust programs. It explores the entire state space, rather than sampling it like Shuttle does. But this means it can only complete when the state space is relatively small, which makes it only viable for smaller programs.
  - Both libraries work similarly, by providing API-compatible alternate implementations of concurrency features such as channels, queues, and mutexes. They recommend conditional compilation to use the stdlib versions in production builds, and the test versions in test builds.
- This is a challenge in Go, because a lot of the concurrency primitives are implemented in the language and runtime rather than in stdlib.

## Future Directions

I do hope to eventually expand repeatable, deterministic testing to a much larger subset of Go programs, even ones not carefully designed around a purely functional core. I have done some experiments with modifying the Go runtime to allow controlling sources of non-determinism, such as setting the random seed that `select` uses, and enabling a mode where the core scheduler logic is deterministic. But a lot more study and work is needed here.

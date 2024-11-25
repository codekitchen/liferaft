# LifeRaft

LifeRaft is a Go implementation of the [Raft consensus protocol](https://raft.github.io/). Raft is a protocol to maintain a consistent linearizable log replicated across a cluster of Raft nodes.

The implementation of Raft isn't really the goal here, though. Raft is being used as a right-sized testbed for exploring modern techniques for testing distributed systems (and locally concurrent systems, but the focus is on distributed).

My primary focus right now is on deterministic simulation testing of distributed systems ([DST](https://notes.eatonphil.com/2024-08-20-deterministic-simulation-testing.html)).

## Current Focus

The core of this Raft implementation is a "pure" deterministic state machine. Given the same state and the same next input, the library will always return the same result. This deterministic approach is key to repeatable testing.

Of course, this precludes doing anything dirty like actually talking to network or disk, or even using timers in some ways. Those concerns are built as layers on top of the core Raft protocol.

This necessarily makes the code a little less idiomatic for Go -- for instance, even a core language feature like `select` is intentionally non-deterministic when more than one channel is ready, so we would have to be very careful about using `select` in the core Raft logic. Right now I'm just not using `select` at all, but I have some ideas there. More sources of non-determinism in Go are covered in [the notes](docs/NOTES.md).

## Status

The core Raft algorithm is implemented. There is a deterministic simulation testing framework starting to take shape in [the tests](raft_test.go). The only failure mode tested so far is network latency, but that has already uncovered a few bugs and helped build confidence in the implementation. The tests check seven invariants from the [etcd raft tla+ spec](https://github.com/etcd-io/raft/blob/main/tla/etcdraft.tla) after each step.

There is also a real-network wrapper which can be run in a 3-node cluster using the [Procfile](Procfile). It provides a simple CLI for a distributed key/value store with `get x` and `set x y` commands. It can be run with [overmind](https://github.com/DarthSim/overmind) and then you can use `overmind connect <procname>` to connect to a CLI and interact with it. Commands are forwarded to the leader node when necessary. It only works ephemerally right now, it doesn't save state to disk.

- [x] Leader Election
- [x] Log Replication
- [x] Log Commits and Applies
- [x] Network Latency Testing
- [x] Add the rest of the tla+ spec invariants
- [x] Simulate node failures/crashes
- [ ] Simulate completely dropped messages
- [ ] Simulate disk failures

## Testing Framework

Since the simulation tests are unbounded and can run for arbitray time, like fuzz tests, they don't run by default. To run the [network test](inmemory_test.go): `go test . -exploreTest TestNetworkBadness -timeout 0`. Add `-v` for verbose status updates.

There is an in-memory cluster implementation that can be used for repeatable testing. I am beginning to build a "chaos framework" on top of this, allowing for things like introducing network delays between specific nodes in the cluster.

I've already seen success with these techniques, finding a bug in leader election when network responses are delayed to a later campaign. It took about 8 minutes running the (very un-optimized) randomized tests and checking the invariants to expose this bug, which otherwise could have very easily made it past other testing techniques and into "production".

But once a failing test case is found, we can re-run it at will! This is such a huge win. Often it's very challenging to come up with a reproducable test case for a bug like this, to the point where many times the fix is implemented without such a test case.

I gave a short 5-minute presentation on finding and fixing this bug, the slides are [here in the repo](docs/first_success.md).

## Further Reading on Deterministic Simulation Techniques

- [Deterministic Simulation link collection](https://asatarin.github.io/testing-distributed-systems/#deterministic-simulation)
- [Testing Distributed Systems w/ Deterministic Simulation](https://www.youtube.com/watch?v=4fFDFbi3toc&feature=youtu.be)
- [Antithesis](https://antithesis.com) is an interesting, very general solution (deterministic VM!) from some of the same people behind FoundationDB. Closed source.
- The [Hermit project](https://github.com/facebookexperimental/hermit) from Facebook tries to accomplish the same thing in a different way, by hooking into syscalls. Development seems to be paused.
- The [rr project](https://rr-project.org) looks like a similar idea to Hermit, but possibly more active. I definitely need to look into this more.
- [A Deterministic Walk Down TigerBeetle’s main() Street](https://www.youtube.com/watch?v=AGxAnkrhDGY) is a quick 15 minute overview of "TigerBeetle Style" deterministic distributed systems programming.
- [Designing Dope Distributed Systems for Outer Space with High-Fidelity Simulation](https://youtu.be/prM-0i58XBM)
- There are a few interesting Rust libraries in this space.
  - [Shuttle](https://github.com/awslabs/shuttle) is an AWS library for randomized deterministic testing of Rust programs. Very cool that the core S3 storage node service is now written in Rust, and tested with Shuttle.
  - [Loom](https://github.com/tokio-rs/loom) is a Tokio project for exhaustive deterministic testing of Rust programs. It explores the entire state space, rather than sampling it like Shuttle does. But this means it can only complete when the state space is relatively small, which makes it only viable for smaller programs.
  - Both libraries work similarly, by providing API-compatible alternate implementations of concurrency features such as channels, queues, and mutexes. They recommend conditional compilation to use the stdlib versions in production builds, and the test versions in test builds.
- This is a challenge in Go, because a lot of the concurrency primitives are implemented in the language and runtime rather than in stdlib.

## Future Directions

I do hope to eventually expand repeatable, deterministic testing to a much larger subset of Go programs, even ones not carefully designed around a purely functional core. I have done some experiments with modifying the Go runtime to allow controlling sources of non-determinism, such as setting the random seed that `select` uses, and enabling a mode where the core scheduler logic is deterministic. But a lot more study and work is needed here. This might be very difficult to do in a way that doesn't affect performance or security, and is acceptable to merge into go core.

I am hoping that my explorations in this repo will help me better understand what that hypothetical more general solution needs to look like.

After the fact, I found [this blog post](https://www.polarsignals.com/blog/posts/2024/05/28/mostly-dst-in-go) from Polar Signals which talks about their adventure in trying to introduce a deterministic mode to Go. They followed most of the same path I did and made many of the same discoveries and decisions. I haven't tried using WASM compiles to work around schedule nondeterminism though.


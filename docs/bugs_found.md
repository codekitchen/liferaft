# Bugs Found by Deterministically Random Testing

## Simulating Network Delays

1. Not rejecting vote responses for previous terms, see [this writeup](first_success.md).
2. Leaders were updating matchIndex even if they didn't send any entries to the follower.
3. Heartbeats weren't including prevLogIndex/Term correctly, so followers were updating their commitIndex to include log entries that they had, but the leader didn't have.


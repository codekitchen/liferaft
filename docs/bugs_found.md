# Bugs Found by Deterministically Random Testing

## Simulating Network Delays

1. Not rejecting vote responses for previous terms, see [this writeup](first_success.md).
2. Leaders were updating matchIndex even if they didn't send any entries to the follower.
3. Heartbeats weren't including prevLogIndex/Term correctly, so followers were updating their commitIndex to include log entries that they had, but the leader didn't have.
4. I was initializing matchIndex to 0 to match the raft paper. But my logs are 0-based not 1-based, so this was implicitly saying each follower's log contained 1 matching entry. Needed to init matchIndex to -1 instead. Caught by `quorumLogInvariant` after 9 minutes.

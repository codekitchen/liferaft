package liferaft

// EphemeralPersistence implements a purely in-memory raft node.
// It is unsafe to have an ephemeral node re-join a cluster once
// stopped.
type EphemeralPersistence struct {
}

// Persist implements RaftPersistence.
func (e *EphemeralPersistence) Persist(state *PersistentState) error {
	return nil
}

// Restore implements RaftPersistence.
func (e *EphemeralPersistence) Restore() (*PersistentState, error) {
	return nil, nil
}

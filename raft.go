package liferaft

import (
	"fmt"
)

type Client interface {
	Apply(cmd []byte) (any, error)
}

type NodeID = string

const NoNode NodeID = ""

type Term uint64

type PersistentState struct {
	CurrentTerm Term
	Log         []Entry
	VotedFor    NodeID
}

type Entry struct {
	Term     Term
	Cmd      []byte
	ClientID string
}

type EntryInfo struct {
	Term  Term
	Index int
}

var NoEntry = EntryInfo{Term: 0, Index: -1}

func (e EntryInfo) GTE(e2 EntryInfo) bool {
	return e.Index >= e2.Index && e.Term >= e2.Term
}

type Role string

const (
	Follower  Role = "follower"
	Candidate Role = "candidate"
	Leader    Role = "leader"
)

// information about a member of the cluster, this Raft node or other node
type Member struct {
	id NodeID
	// index of next log entry to send to that server
	nextIndex int
	// index of highest log entry known to be replicated on server
	matchIndex int
	votedFor   NodeID
}

// Raft is a raft node instance, implemented as a pure state machine.
// This type is not thread-safe, you must call all methods from the same goroutine.
type Raft struct {
	// static state
	id     NodeID
	client Client
	// persistent state
	currentTerm Term
	log         []Entry
	// volatile state on all servers
	committedLength int
	appliedLength   int
	members         []*Member
	selfMember      *Member
	role            Role
	leaderID        NodeID
	// ticks
	heartBeatTick       uint
	electionTimeoutTick uint
	ticks               uint
}

type RaftConfig struct {
	ID                  NodeID
	Client              Client
	Cluster             []NodeID // must contain self as well
	HeartBeatTick       uint
	ElectionTimeoutTick uint
	RestoreState        *PersistentState // existing node, restore from this state
}

func NewRaft(config *RaftConfig) *Raft {
	raft := &Raft{
		id:     NodeID(config.ID),
		client: config.Client,

		currentTerm: Term(1),

		members:  make([]*Member, len(config.Cluster)),
		role:     Follower,
		leaderID: NoNode,

		heartBeatTick:       1,
		electionTimeoutTick: 10,
	}
	if config.HeartBeatTick > 0 {
		raft.heartBeatTick = config.HeartBeatTick
	}
	if config.ElectionTimeoutTick > 0 {
		raft.electionTimeoutTick = config.ElectionTimeoutTick
	}
	for i, id := range config.Cluster {
		raft.members[i] = &Member{
			id:         id,
			nextIndex:  0,
			matchIndex: -1,
			votedFor:   NoNode,
		}
		if id == config.ID {
			raft.selfMember = raft.members[i]
		}
	}

	if config.RestoreState != nil {
		raft.restoreFromState(config.RestoreState)
	}

	return raft
}

func (s *Raft) IsLeader() bool {
	return s.role == Leader
}

func (s *Raft) LeaderID() NodeID {
	return s.leaderID
}

type Message struct {
	From, To NodeID
	Term     Term
	Contents RPC
}
type Tick struct{}
type Apply struct {
	Cmd      []byte
	ClientID string
}

// This union type is verbose in go, but it's easier to reason about testing
// when an Event can't be both a message and a tick at the same time.
type Event interface {
	isRaftEvent()
}

// Ditto. Verbose but precludes invalid RPCs.
type RPC interface {
	isRaftRPC()
}

func (t *Tick) isRaftEvent()    {}
func (m *Message) isRaftEvent() {}
func (m *Apply) isRaftEvent()   {}

type RequestVote struct {
	LastLogEntry EntryInfo
}

type RequestVoteResponse struct {
	VoteGranted bool
}

type AppendEntries struct {
	PrevLogEntry          EntryInfo
	Entries               []Entry
	LeaderCommittedLength int
}
type AppendEntriesResponse struct {
	Success          bool
	LastIndexApplied int
	// The leader doesn't keep track of if the AppendEntries request
	// included any entries, or if it was just a heartbeat.
	// So we have to relay that information back to it.
	SentEntries bool
}

// gossip apply to the leader node
type RelayApply struct {
	Apply *Apply
}

func (r *RequestVote) isRaftRPC()           {}
func (r *RequestVoteResponse) isRaftRPC()   {}
func (r *AppendEntries) isRaftRPC()         {}
func (r *AppendEntriesResponse) isRaftRPC() {}
func (r *RelayApply) isRaftRPC()            {}

type Updates struct {
	Persist  *PersistentState
	Apply    []Entry
	Outgoing []*Message
}

func (s *Raft) HandleEvent(event Event) Updates {
	var updates Updates
	var ms []*Message
	switch event := event.(type) {
	case *Tick:
		s.ticks++
		if s.role == Leader && s.ticks >= s.heartBeatTick {
			ms = s.sendHeartbeat()
		} else if s.ticks >= s.electionTimeoutTick {
			ms = s.startElection()
		}
	case *Message:
		// If a server receives a request with a stale term number,
		// it rejects the request. (§5.1)
		if event.Term < s.currentTerm {
			break
		}
		// must persist state before responding to RPC
		updates.Persist = &PersistentState{
			CurrentTerm: s.currentTerm,
			Log:         s.log,
			VotedFor:    s.selfMember.votedFor,
		}
		// If message term > currentTerm, update currentTerm
		// and convert to follower before responding
		if event.Term > s.currentTerm {
			s.updateTerm(event.Term)
			s.role = Follower
		}

		switch rpc := event.Contents.(type) {
		case *RequestVote:
			ms = s.handleRequestVote(event, rpc)
		case *RequestVoteResponse:
			ms = s.handleRequestVoteResponse(event, rpc)
		case *AppendEntries:
			ms = s.handleAppendEntries(event, rpc)
		case *AppendEntriesResponse:
			ms = s.handleAppendEntriesResponse(event, rpc)
		case *RelayApply:
			if s.role == Leader {
				s.log = append(s.log, Entry{
					Term:     s.currentTerm,
					Cmd:      rpc.Apply.Cmd,
					ClientID: rpc.Apply.ClientID,
				})
				ms = s.sendHeartbeat()
			}
			// if we aren't leader anymore, have to drop it and wait for timeout/retry
		}
	case *Apply:
		if s.role == Leader {
			s.log = append(s.log, Entry{
				Term:     s.currentTerm,
				Cmd:      event.Cmd,
				ClientID: event.ClientID,
			})
			ms = s.sendHeartbeat()
		} else {
			ms = s.relayApplyToLeader(event)
		}
	default:
		panic(fmt.Sprintf("invalid type passed to HandleEvent %#v", event))
	}
	// If commitIndex > lastApplied: increment lastApplied, apply log[lastAppiled] to state machine (§5.3)
	if s.appliedLength < s.committedLength {
		updates.Apply = s.log[s.appliedLength:s.committedLength]
		s.appliedLength = s.committedLength
	}

	updates.Outgoing = append(updates.Outgoing, ms...)
	return updates
}

func (s *Raft) startElection() (ms []*Message) {
	s.role = Candidate
	s.updateTerm(s.currentTerm + 1)
	ms = append(ms, s.gotVote(s.id)...) // got our own vote!
	ms = append(ms, s.sendToAllButSelf(&RequestVote{
		LastLogEntry: s.logStatus(),
	})...)
	return
}

func (s *Raft) sendToAllButSelf(rpc RPC) (ms []*Message) {
	for _, m := range s.members {
		if s.selfMember == m {
			continue
		}
		ms = append(ms, &Message{
			From:     s.id,
			To:       m.id,
			Term:     s.currentTerm,
			Contents: rpc,
		})
	}
	return
}

func (s *Raft) handleRequestVote(msg *Message, req *RequestVote) (ms []*Message) {
	res := &RequestVoteResponse{}
	ms = []*Message{{
		From:     s.id,
		To:       msg.From,
		Term:     s.currentTerm,
		Contents: res,
	}}
	// If votedFor is null or candidateId, and candidate’s log is at least as up-to-date as receiver’s log
	// grant vote (§5.2,§5.4)
	if s.selfMember.votedFor == "" || s.selfMember.votedFor == msg.From {
		myLastEntry := s.logStatus()
		if req.LastLogEntry.GTE(myLastEntry) {
			res.VoteGranted = true
			s.ticks = 0
			s.selfMember.votedFor = msg.From
		}
	}
	return
}

func (s *Raft) handleRequestVoteResponse(msg *Message, req *RequestVoteResponse) []*Message {
	if s.role != Candidate {
		return nil
	}
	if req.VoteGranted {
		return s.gotVote(msg.From)
	}
	return nil
}

func (s *Raft) gotVote(from NodeID) []*Message {
	s.member(from).votedFor = s.id
	// If votes received from majority of servers : become leader
	meCount := s.voteCount(s.id)
	if meCount >= s.quorumSize() {
		s.winElection()
		// send empty AppendEntries RPC to each server
		return s.sendHeartbeat()
	}
	return nil
}

func (s *Raft) member(id NodeID) *Member {
	for _, m := range s.members {
		if m.id == id {
			return m
		}
	}
	return nil
}

func (s *Raft) voteCount(forNode NodeID) (count int) {
	for _, m := range s.members {
		if m.votedFor == forNode {
			count++
		}
	}
	return
}

func (s *Raft) handleAppendEntries(msg *Message, req *AppendEntries) (ms []*Message) {
	res := &AppendEntriesResponse{}
	ms = []*Message{{
		From:     s.id,
		To:       msg.From,
		Term:     s.currentTerm,
		Contents: res,
	}}

	if s.role == Candidate {
		// If AppendEntriesRPC received from new leader: convert to follower
		s.role = Follower
	}
	s.ticks = 0
	s.leaderID = msg.From

	if !s.hasMatchingLogEntry(req.PrevLogEntry) {
		return
	}

	// If an existing entry conflicts with a new one (same index
	// but different terms), delete the existing entry and all that follow it (§5.3)
	for i, e := range req.Entries {
		existingIdx := req.PrevLogEntry.Index + 1 + i
		if existingIdx >= len(s.log) {
			break
		}
		if s.log[existingIdx].Term != e.Term {
			// delete the existing entry and all that follow it
			// TODO: notify the client that these commands were dropped, so that it can
			// quickly retry if it cares about any of them, instead of waiting for timeout.
			s.log = s.log[0:existingIdx]
			break
		}
	}

	// append any new entries not already in the log
	s.log = appendNewEntries(s.log, req.PrevLogEntry.Index+1, req.Entries)
	s.selfMember.matchIndex = len(s.log) - 1

	if req.LeaderCommittedLength > s.committedLength {
		s.committedLength = min(req.LeaderCommittedLength, len(s.log))
	}

	if len(req.Entries) > 0 {
		res.SentEntries = true
		res.LastIndexApplied = len(s.log) - 1
	}
	res.Success = true
	return
}

func appendNewEntries[T any](log []T, firstNewIdx int, newEntries []T) []T {
	alreadyHave := len(log) - firstNewIdx
	if alreadyHave < len(newEntries) {
		log = append(log, newEntries[alreadyHave:]...)
	}
	return log
}

func (s *Raft) hasMatchingLogEntry(entry EntryInfo) bool {
	if entry == NoEntry {
		return true
	}
	if entry.Index >= len(s.log) {
		return false
	}
	return s.log[entry.Index].Term == entry.Term
}

func (s *Raft) handleAppendEntriesResponse(msg *Message, req *AppendEntriesResponse) (ms []*Message) {
	m := s.member(msg.From)
	if req.Success {
		if req.SentEntries {
			m.matchIndex = req.LastIndexApplied
			m.nextIndex = req.LastIndexApplied + 1
		}
	} else {
		m.nextIndex = max(0, m.nextIndex-1)
		// TODO: retry immediately, this retries on next heartbeat
	}
	s.checkForCommits()
	return
}

func (s *Raft) checkForCommits() {
	// If there exists an N such that N > commitIndex, a majority of matchIndex[n] >= N, and log[N].term == currentTerm:
	// set commitIndex = N
	for n := len(s.log) - 1; n > s.committedLength-1; n-- {
		entry := s.log[n]
		if entry.Term != s.currentTerm {
			break
		}
		count := 0
		for _, m := range s.members {
			if m.matchIndex >= n {
				count++
			}
		}
		if count >= s.quorumSize() {
			s.committedLength = n + 1
			break
		}
	}
}

func (s *Raft) logStatus() EntryInfo {
	if len(s.log) == 0 {
		return NoEntry
	}
	return EntryInfo{
		Term:  s.log[len(s.log)-1].Term,
		Index: len(s.log) - 1,
	}
}

func (s *Raft) winElection() {
	s.role = Leader
	for _, m := range s.members {
		m.nextIndex = len(s.log)
		m.matchIndex = -1
	}
}

func (s *Raft) quorumSize() int {
	return quorumSize(len(s.members))
}

func quorumSize(n int) int {
	return n/2 + 1
}

func (s *Raft) updateTerm(term Term) {
	s.currentTerm = term
	s.ticks = 0
	for _, m := range s.members {
		m.votedFor = NoNode
	}
}

// CommittedLog returns the portion of the log that has been committed.
// (but not necessarily applied).
func (s *Raft) CommittedLog() []Entry {
	return s.log[0:s.committedLength]
}

// sendHeartbeat currently *always* replicates log entries
// as well. This ensures that the leader continues to retry
// replication if the follower doesn't respond.
// In practice this might be too chatty, though, given
// that the heartbeat timeout could be smaller than the
// time it takes for a follower to apply AppendEntries and
// reply.
// Future improvement: separate tick timeout for retrying
// AppendEntries when the leader gets no response, then
// heartbeats can go back to not replicating logs.
func (s *Raft) sendHeartbeat() (ms []*Message) {
	s.ticks = 0

	for _, n := range s.members {
		if n.id == s.id {
			continue
		}
		rpc := &AppendEntries{
			LeaderCommittedLength: s.committedLength,
			PrevLogEntry:          NoEntry,
		}
		rpc.Entries = s.log[n.nextIndex:]
		prevIndex := n.nextIndex - 1
		if prevIndex >= 0 {
			rpc.PrevLogEntry = EntryInfo{
				Index: prevIndex,
				Term:  s.log[prevIndex].Term,
			}
		}
		ms = append(ms, &Message{
			From:     s.id,
			To:       n.id,
			Term:     s.currentTerm,
			Contents: rpc,
		})
	}
	return
}

func (s *Raft) relayApplyToLeader(apply *Apply) (ms []*Message) {
	if s.leaderID == NoNode {
		// would be nice to get this back to the client somehow,
		// so they can retry sooner than their overall apply timeout.
		return
	}

	ms = append(ms, &Message{
		From: s.id,
		To:   s.leaderID,
		Term: s.currentTerm,
		Contents: &RelayApply{
			Apply: apply,
		},
	})

	return
}

func (s *Raft) restoreFromState(state *PersistentState) {
	s.currentTerm = state.CurrentTerm
	s.log = state.Log
	s.selfMember.votedFor = state.VotedFor
}

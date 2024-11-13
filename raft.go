package liferaft

import "log/slog"

type Client interface {
	Apply(cmd []byte) ([]byte, error)
}

type NodeID string
type Term uint64

const NoNode NodeID = ""

type PersistentState struct {
	CurrentTerm Term
	Log         []Entry
	VotedFor    NodeID
}

type Entry struct {
	Term Term
	Cmd  []byte
}

type Role string

const (
	Follower  Role = "follower"
	Candidate Role = "candidate"
	Leader    Role = "leader"
)

// information about a member of the cluster, this Raft node or other node
type Member struct {
	nextIndex  uint64
	matchIndex uint64
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
	commitIndex uint64
	lastApplied uint64
	// volatile state on leader
	role    Role
	members map[NodeID]*Member
	// ticks
	heartBeatTick       uint
	electionTimeoutTick uint
	ticks               uint
}

type RaftConfig struct {
	ID      NodeID
	Client  Client
	Cluster []NodeID // must contain self as well
}

func NewRaft(config *RaftConfig) *Raft {
	raft := &Raft{
		id:     NodeID(config.ID),
		client: config.Client,

		role:    Follower,
		members: make(map[NodeID]*Member),

		heartBeatTick:       1,
		electionTimeoutTick: 10,
	}
	raft.members[raft.id] = &Member{
		nextIndex:  1,
		matchIndex: 0,
	}
	for _, id := range config.Cluster {
		raft.members[id] = &Member{
			nextIndex:  1,
			matchIndex: 0,
		}
	}
	return raft
}

// server is state machine -- takes event which could be timer or message from peer, returns new state
// how does that look to the TUI client?
type Message struct {
	From, To NodeID
	Term     Term
	Contents RPC
}
type Tick struct{}

// this is verbose, but it's easier to reason about testing when an Event can't be
// both a message and a tick
type Event interface {
	isRaftEvent()
}

type RPC interface {
	isRaftRPC()
}

func (t *Tick) isRaftEvent()    {}
func (m *Message) isRaftEvent() {}

type RequestVote struct {
	Term         Term
	LastLogIndex uint64
	LastLogTerm  Term
}

type RequestVoteResponse struct {
	Term        Term
	VoteGranted bool
}

type AppendEntries struct{}
type AppendEntriesResponse struct{}

func (r *RequestVote) isRaftRPC()           {}
func (r *RequestVoteResponse) isRaftRPC()   {}
func (r *AppendEntries) isRaftRPC()         {}
func (r *AppendEntriesResponse) isRaftRPC() {}

type Updates struct {
	Persist  *PersistentState
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
		// must persist state before responding to RPC
		updates.Persist = &PersistentState{
			CurrentTerm: s.currentTerm,
			Log:         s.log,
			VotedFor:    s.members[s.id].votedFor,
		}
		// TODO: if message term > currentTerm, update currentTerm
		// and convert to follower before responding
		switch rpc := event.Contents.(type) {
		case *RequestVote:
			ms = s.handleRequestVote(event, rpc)
		case *RequestVoteResponse:
			ms = s.handleRequestVoteResponse(event, rpc)
		case *AppendEntries:
			ms = s.handleAppendEntries(event, rpc)
		case *AppendEntriesResponse:
			ms = s.handleAppendEntriesResponse(event, rpc)
		}
	default:
		panic("invalid type passed to HandleEvent")
	}
	updates.Outgoing = append(updates.Outgoing, ms...)
	return updates
}

func (s *Raft) startElection() (ms []*Message) {
	s.role = Candidate
	s.updateTerm(s.currentTerm + 1)
	s.slog().Info("starting election")
	ms = append(ms, s.gotVote(s.id)...) // got our own vote!
	lastLogIndex, lastLogTerm := s.logStatus()
	ms = append(ms, s.sendToAllButSelf(&RequestVote{
		Term:         s.currentTerm,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	})...)
	return
}

func (s *Raft) sendToAllButSelf(rpc RPC) (ms []*Message) {
	for id := range s.members {
		if id == s.id {
			continue
		}
		ms = append(ms, &Message{
			From:     s.id,
			To:       id,
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
	// Reply false if term < currentTerm (§5.1)
	if req.Term < s.currentTerm {
		return
	}
	// If votedFor is null or candidateId, and candidate’s log is at least as up-to-date as receiver’s log
	// grant vote (§5.2,§5.4)
	if s.members[s.id].votedFor == "" || s.members[s.id].votedFor == msg.From {
		lastLogIndex, lastLogTerm := s.logStatus()
		if req.LastLogIndex >= lastLogIndex && req.LastLogTerm >= lastLogTerm {
			res.VoteGranted = true
			s.ticks = 0
			s.members[s.id].votedFor = msg.From
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
	s.members[from].votedFor = s.id
	// If votes received from majority of servers : become leader
	meCount := s.voteCount(s.id)
	s.slog().Info("vote response", "from", from, "meCount", meCount, "quorumSize", s.quorumSize())
	if meCount >= s.quorumSize() {
		s.winElection()
		// send empty AppendEntries RPC to each server
		return s.sendHeartbeat()
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
	// heartbeat
	if s.role == Candidate {
		// If AppendEntriesRPC received from new leader: convert to follower
		s.role = Follower
	}
	s.ticks = 0
	return
}

func (s *Raft) handleAppendEntriesResponse(msg *Message, req *AppendEntriesResponse) (ms []*Message) {
	panic("implement me")
}

func (s *Raft) logStatus() (lastLogIndex uint64, lastLogTerm Term) {
	if len(s.log) > 0 {
		lastLogIndex = uint64(len(s.log)) - 1
		lastLogTerm = s.log[len(s.log)-1].Term
	}
	return
}

func (s *Raft) Apply(cmd []byte) ([]byte, error) {
	panic("implement Apply, remember thread safety")
}

func (s *Raft) winElection() {
	s.role = Leader
	s.slog().Info("won election", "votes", s.voteCount(s.id))
}

func (s *Raft) quorumSize() int {
	return len(s.members)/2 + 1
}

func (s *Raft) slog() *slog.Logger {
	return slog.With(slog.Group("raft", "currentTerm", s.currentTerm, "role", s.role))
}

func (s *Raft) updateTerm(term Term) {
	s.currentTerm = term
	s.ticks = 0
	for _, m := range s.members {
		m.votedFor = NoNode
	}
}

func (s *Raft) sendHeartbeat() []*Message {
	s.ticks = 0
	return s.sendToAllButSelf(&AppendEntries{})
}

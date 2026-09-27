// Package raft implements the Raft consensus algorithm from scratch.
//
// This is a from-first-principles implementation covering the three core
// sub-problems described in the Raft paper (Ongaro & Ousterhout, 2014):
//
//  1. Leader election via randomized election timeouts and RequestVote RPCs.
//  2. Log replication via AppendEntries RPCs with prev-log consistency checks.
//  3. Safety: the commit-index only advances for entries in the leader's
//     current term (Figure 8 constraint), guaranteeing linearizable state.
//
// The implementation is deliberately transport-agnostic: RPCs are defined as
// plain Go structs and dispatched through a Transport interface, so the same
// core can run over HTTP, gRPC, or an in-memory test harness.
package raft

import "time"

// Role is the current role of a Raft node in the cluster.
type Role int

const (
	// Follower passively replicates entries from the leader.
	Follower Role = iota
	// Candidate is attempting to win an election.
	Candidate
	// Leader handles all client writes and replicates them.
	Leader
)

func (r Role) String() string {
	switch r {
	case Follower:
		return "follower"
	case Candidate:
		return "candidate"
	case Leader:
		return "leader"
	default:
		return "unknown"
	}
}

// LogEntry is a single replicated command in the Raft log.
//
// Term is the leader term in which the entry was created; it is used for the
// log-matching property. Index is the 1-based position in the log. Command is
// an opaque byte slice that the application state machine interprets.
type LogEntry struct {
	Term    uint64 `json:"term"`
	Index   uint64 `json:"index"`
	Command []byte `json:"command"`
}

// RequestVoteArgs are the arguments for the RequestVote RPC (paper §5.2).
type RequestVoteArgs struct {
	Term         uint64 `json:"term"`
	CandidateID  string `json:"candidate_id"`
	LastLogIndex uint64 `json:"last_log_index"`
	LastLogTerm  uint64 `json:"last_log_term"`
}

// RequestVoteReply is the response to a RequestVote RPC.
type RequestVoteReply struct {
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
}

// AppendEntriesArgs are the arguments for the AppendEntries RPC (paper §5.3).
// An empty Entries slice is used as a heartbeat.
type AppendEntriesArgs struct {
	Term         uint64     `json:"term"`
	LeaderID     string     `json:"leader_id"`
	PrevLogIndex uint64     `json:"prev_log_index"`
	PrevLogTerm  uint64     `json:"prev_log_term"`
	Entries      []LogEntry `json:"entries"`
	LeaderCommit uint64     `json:"leader_commit"`
}

// AppendEntriesReply is the response to an AppendEntries RPC.
//
// ConflictIndex and ConflictTerm implement the fast log-backtracking
// optimization described in the paper, allowing a leader to skip past an
// entire conflicting term in one round trip instead of decrementing nextIndex
// one entry at a time.
type AppendEntriesReply struct {
	Term          uint64 `json:"term"`
	Success       bool   `json:"success"`
	ConflictIndex uint64 `json:"conflict_index"`
	ConflictTerm  uint64 `json:"conflict_term"`
}

// Transport abstracts the network layer so the Raft core can be tested
// deterministically in-memory and run over a real network in production.
type Transport interface {
	SendRequestVote(target string, args *RequestVoteArgs) (*RequestVoteReply, error)
	SendAppendEntries(target string, args *AppendEntriesArgs) (*AppendEntriesReply, error)
}

// ApplyMsg is delivered to the application state machine when an entry is
// committed. The state machine is expected to apply Command deterministically.
type ApplyMsg struct {
	Index   uint64
	Term    uint64
	Command []byte
}

// Config holds tunable timing parameters for a Raft node. Election timeouts
// are randomized in [ElectionTimeoutMin, ElectionTimeoutMax) to avoid split
// votes, and heartbeats are sent at HeartbeatInterval by the leader.
type Config struct {
	ElectionTimeoutMin time.Duration
	ElectionTimeoutMax time.Duration
	HeartbeatInterval  time.Duration
}

// DefaultConfig returns timing parameters suitable for a small cluster on a
// low-latency network (e.g. within an AWS availability zone).
func DefaultConfig() Config {
	return Config{
		ElectionTimeoutMin: 300 * time.Millisecond,
		ElectionTimeoutMax: 600 * time.Millisecond,
		HeartbeatInterval:  100 * time.Millisecond,
	}
}

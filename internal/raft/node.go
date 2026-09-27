package raft

import (
	"math/rand"
	"sync"
	"time"
)

// Node is a single Raft server. It runs its own goroutine-driven state machine
// and is safe for concurrent access; all mutable state is guarded by mu.
type Node struct {
	mu sync.Mutex

	id    string   // stable unique id of this node
	peers []string // ids of all *other* nodes in the cluster

	cfg       Config
	transport Transport
	applyCh   chan ApplyMsg

	// Persistent state (would be flushed to stable storage in production;
	// here it is kept in memory and reconstructed via the WAL in the store
	// layer, which is sufficient to demonstrate the algorithm).
	currentTerm uint64
	votedFor    string
	log         []LogEntry // 1-based; log[0] is a sentinel

	// Volatile state on all servers.
	role        Role
	commitIndex uint64
	lastApplied uint64
	leaderID    string

	// Volatile state on leaders, reset after every election.
	nextIndex  map[string]uint64
	matchIndex map[string]uint64

	// electionResetAt is the last time we heard from a valid leader or granted
	// a vote; the election ticker fires if we go silent for a full timeout.
	electionResetAt time.Time
	electionTimeout time.Duration

	stopCh chan struct{}
	rng    *rand.Rand
}

// NewNode constructs a follower that has not yet started its background loops.
// Call Start to begin participating in the cluster.
func NewNode(id string, peers []string, cfg Config, tr Transport, applyCh chan ApplyMsg) *Node {
	n := &Node{
		id:         id,
		peers:      peers,
		cfg:        cfg,
		transport:  tr,
		applyCh:    applyCh,
		role:       Follower,
		log:        []LogEntry{{Term: 0, Index: 0}}, // sentinel at index 0
		nextIndex:  make(map[string]uint64),
		matchIndex: make(map[string]uint64),
		stopCh:     make(chan struct{}),
		rng:        rand.New(rand.NewSource(time.Now().UnixNano() + int64(len(id)))),
	}
	n.resetElectionTimer()
	return n
}

// Start launches the election ticker. Heartbeats are only sent once elected.
func (n *Node) Start() {
	go n.ticker()
}

// Stop halts all background goroutines.
func (n *Node) Stop() {
	close(n.stopCh)
}

// --- Accessors used by the API/dashboard layer -----------------------------

// Status is a snapshot of node state for observability.
type Status struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	Term        uint64 `json:"term"`
	LeaderID    string `json:"leader_id"`
	CommitIndex uint64 `json:"commit_index"`
	LastApplied uint64 `json:"last_applied"`
	LogLength   uint64 `json:"log_length"`
	VotedFor    string `json:"voted_for"`
}

// Status returns a consistent snapshot of the node's state.
func (n *Node) Status() Status {
	n.mu.Lock()
	defer n.mu.Unlock()
	return Status{
		ID:          n.id,
		Role:        n.role.String(),
		Term:        n.currentTerm,
		LeaderID:    n.leaderID,
		CommitIndex: n.commitIndex,
		LastApplied: n.lastApplied,
		LogLength:   uint64(len(n.log) - 1),
		VotedFor:    n.votedFor,
	}
}

// IsLeader reports whether this node currently believes it is the leader.
func (n *Node) IsLeader() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.role == Leader
}

// LeaderID returns the id of the node this server currently believes is leader.
func (n *Node) LeaderID() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.leaderID
}

// Propose appends a command to the log if this node is the leader. It returns
// the index the command will occupy and true, or (0, false) if not leader.
// The command is only guaranteed to be applied once it is committed and
// delivered on applyCh.
func (n *Node) Propose(command []byte) (uint64, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.role != Leader {
		return 0, false
	}
	entry := LogEntry{
		Term:    n.currentTerm,
		Index:   n.lastIndex() + 1,
		Command: command,
	}
	n.log = append(n.log, entry)
	// Replicate immediately rather than waiting for the next heartbeat.
	go n.broadcastAppendEntries()
	return entry.Index, true
}

// --- Election timing --------------------------------------------------------

func (n *Node) resetElectionTimer() {
	span := n.cfg.ElectionTimeoutMax - n.cfg.ElectionTimeoutMin
	n.electionTimeout = n.cfg.ElectionTimeoutMin + time.Duration(n.rng.Int63n(int64(span)))
	n.electionResetAt = time.Now()
}

// ticker is the single background loop that drives elections and heartbeats.
func (n *Node) ticker() {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-n.stopCh:
			return
		case <-tick.C:
			n.mu.Lock()
			switch n.role {
			case Leader:
				n.mu.Unlock()
				n.broadcastAppendEntries()
			default:
				elapsed := time.Since(n.electionResetAt)
				if elapsed >= n.electionTimeout {
					n.mu.Unlock()
					n.startElection()
				} else {
					n.mu.Unlock()
				}
			}
		}
	}
}

// --- Helpers (must be called with mu held) ---------------------------------

func (n *Node) lastIndex() uint64 { return n.log[len(n.log)-1].Index }
func (n *Node) lastTerm() uint64  { return n.log[len(n.log)-1].Term }

func (n *Node) quorum() int { return (len(n.peers)+1)/2 + 1 }

// becomeFollower steps down to follower for the given (higher-or-equal) term.
func (n *Node) becomeFollower(term uint64, leader string) {
	n.role = Follower
	n.currentTerm = term
	n.votedFor = ""
	n.leaderID = leader
	n.resetElectionTimer()
}

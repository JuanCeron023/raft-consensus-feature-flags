package raft

import "sync"

// startElection transitions to candidate, increments the term, votes for self,
// and requests votes from all peers in parallel (paper §5.2).
func (n *Node) startElection() {
	n.mu.Lock()
	n.role = Candidate
	n.currentTerm++
	n.votedFor = n.id
	n.leaderID = ""
	n.resetElectionTimer()
	term := n.currentTerm
	args := &RequestVoteArgs{
		Term:         term,
		CandidateID:  n.id,
		LastLogIndex: n.lastIndex(),
		LastLogTerm:  n.lastTerm(),
	}
	n.mu.Unlock()

	var (
		mu    sync.Mutex
		votes = 1 // vote for self
	)
	for _, peer := range n.peers {
		go func(peer string) {
			reply, err := n.transport.SendRequestVote(peer, args)
			if err != nil {
				return
			}
			n.mu.Lock()
			defer n.mu.Unlock()

			// If we've moved on (new term or no longer candidate), ignore.
			if n.role != Candidate || n.currentTerm != term {
				return
			}
			if reply.Term > n.currentTerm {
				n.becomeFollower(reply.Term, "")
				return
			}
			if !reply.VoteGranted {
				return
			}
			mu.Lock()
			votes++
			won := votes >= n.quorum()
			mu.Unlock()
			if won {
				n.becomeLeader()
			}
		}(peer)
	}
}

// becomeLeader initializes leader volatile state and sends an immediate round
// of heartbeats to assert authority. Must be called with mu held.
func (n *Node) becomeLeader() {
	if n.role != Candidate {
		return
	}
	n.role = Leader
	n.leaderID = n.id
	next := n.lastIndex() + 1
	for _, peer := range n.peers {
		n.nextIndex[peer] = next
		n.matchIndex[peer] = 0
	}
	go n.broadcastAppendEntries()
}

// HandleRequestVote processes an incoming RequestVote RPC (paper §5.2, §5.4.1).
// A vote is granted only if the candidate's term is current and its log is at
// least as up-to-date as the receiver's log.
func (n *Node) HandleRequestVote(args *RequestVoteArgs) *RequestVoteReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply := &RequestVoteReply{Term: n.currentTerm, VoteGranted: false}

	if args.Term < n.currentTerm {
		return reply
	}
	if args.Term > n.currentTerm {
		n.becomeFollower(args.Term, "")
	}

	// Up-to-date check: candidate log must be at least as current as ours.
	logOK := args.LastLogTerm > n.lastTerm() ||
		(args.LastLogTerm == n.lastTerm() && args.LastLogIndex >= n.lastIndex())

	canVote := n.votedFor == "" || n.votedFor == args.CandidateID
	if canVote && logOK {
		n.votedFor = args.CandidateID
		n.resetElectionTimer()
		reply.VoteGranted = true
	}
	reply.Term = n.currentTerm
	return reply
}

// broadcastAppendEntries sends AppendEntries (or heartbeats) to all peers,
// tailoring each RPC to that peer's nextIndex.
func (n *Node) broadcastAppendEntries() {
	n.mu.Lock()
	if n.role != Leader {
		n.mu.Unlock()
		return
	}
	term := n.currentTerm
	n.mu.Unlock()

	for _, peer := range n.peers {
		go n.replicateTo(peer, term)
	}
}

func (n *Node) replicateTo(peer string, term uint64) {
	n.mu.Lock()
	if n.role != Leader || n.currentTerm != term {
		n.mu.Unlock()
		return
	}
	prevIndex := n.nextIndex[peer] - 1
	prevTerm := n.log[prevIndex].Term
	entries := append([]LogEntry(nil), n.log[prevIndex+1:]...)
	args := &AppendEntriesArgs{
		Term:         term,
		LeaderID:     n.id,
		PrevLogIndex: prevIndex,
		PrevLogTerm:  prevTerm,
		Entries:      entries,
		LeaderCommit: n.commitIndex,
	}
	n.mu.Unlock()

	reply, err := n.transport.SendAppendEntries(peer, args)
	if err != nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.role != Leader || n.currentTerm != term {
		return
	}
	if reply.Term > n.currentTerm {
		n.becomeFollower(reply.Term, "")
		return
	}
	if reply.Success {
		n.matchIndex[peer] = prevIndex + uint64(len(entries))
		n.nextIndex[peer] = n.matchIndex[peer] + 1
		n.advanceCommitIndex()
		return
	}
	// Fast backtracking using the conflict hint.
	if reply.ConflictTerm == 0 {
		n.nextIndex[peer] = reply.ConflictIndex
	} else {
		// Find the last entry we have for ConflictTerm.
		idx := prevIndex
		for idx > 0 && n.log[idx].Term != reply.ConflictTerm {
			idx--
		}
		if idx == 0 {
			n.nextIndex[peer] = reply.ConflictIndex
		} else {
			n.nextIndex[peer] = idx + 1
		}
	}
	if n.nextIndex[peer] < 1 {
		n.nextIndex[peer] = 1
	}
}

// advanceCommitIndex advances commitIndex to the highest N such that a majority
// of matchIndex[i] >= N AND log[N].Term == currentTerm. The term check is the
// Figure 8 safety constraint: a leader never commits entries from prior terms
// by counting replicas; those are committed indirectly once a current-term
// entry is committed. Must be called with mu held.
func (n *Node) advanceCommitIndex() {
	for idx := n.lastIndex(); idx > n.commitIndex; idx-- {
		if n.log[idx].Term != n.currentTerm {
			continue
		}
		count := 1 // leader replicates to itself
		for _, peer := range n.peers {
			if n.matchIndex[peer] >= idx {
				count++
			}
		}
		if count >= n.quorum() {
			n.commitIndex = idx
			n.applyCommitted()
			return
		}
	}
}

// HandleAppendEntries processes an incoming AppendEntries RPC (paper §5.3).
func (n *Node) HandleAppendEntries(args *AppendEntriesArgs) *AppendEntriesReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply := &AppendEntriesReply{Term: n.currentTerm, Success: false}

	if args.Term < n.currentTerm {
		return reply
	}
	// Valid leader for this term: (re)synchronize to follower state.
	n.becomeFollower(args.Term, args.LeaderID)
	reply.Term = n.currentTerm

	// Consistency check: our log must contain PrevLogIndex with PrevLogTerm.
	if args.PrevLogIndex > n.lastIndex() {
		reply.ConflictIndex = n.lastIndex() + 1
		reply.ConflictTerm = 0
		return reply
	}
	if n.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		reply.ConflictTerm = n.log[args.PrevLogIndex].Term
		// First index of the conflicting term, for fast backtracking.
		i := args.PrevLogIndex
		for i > 0 && n.log[i-1].Term == reply.ConflictTerm {
			i--
		}
		reply.ConflictIndex = i
		return reply
	}

	// Append any new entries, truncating on the first conflicting index.
	for i, entry := range args.Entries {
		idx := args.PrevLogIndex + 1 + uint64(i)
		if idx <= n.lastIndex() {
			if n.log[idx].Term == entry.Term {
				continue
			}
			n.log = n.log[:idx] // truncate conflicting suffix
		}
		n.log = append(n.log, entry)
	}

	if args.LeaderCommit > n.commitIndex {
		n.commitIndex = min(args.LeaderCommit, n.lastIndex())
		n.applyCommitted()
	}
	reply.Success = true
	return reply
}

// applyCommitted delivers newly committed entries to the state machine in
// order. Must be called with mu held; delivery is non-blocking-safe because
// applyCh is buffered by the caller.
func (n *Node) applyCommitted() {
	for n.lastApplied < n.commitIndex {
		n.lastApplied++
		entry := n.log[n.lastApplied]
		msg := ApplyMsg{Index: entry.Index, Term: entry.Term, Command: entry.Command}
		// Deliver without holding the lock across a potentially blocking send
		// would be ideal; the buffered channel keeps this simple and correct
		// for the cluster sizes this project targets.
		select {
		case n.applyCh <- msg:
		default:
			go func(m ApplyMsg) { n.applyCh <- m }(msg)
		}
	}
}

func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

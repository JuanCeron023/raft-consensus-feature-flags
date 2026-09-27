package raft

import (
	"sync"
	"testing"
	"time"
)

// memNetwork is an in-memory transport that wires nodes together and supports
// partitioning individual nodes to test failover deterministically.
type memNetwork struct {
	mu        sync.RWMutex
	nodes     map[string]*Node
	partition map[string]bool // id -> isolated
}

func newMemNetwork() *memNetwork {
	return &memNetwork{
		nodes:     make(map[string]*Node),
		partition: make(map[string]bool),
	}
}

func (m *memNetwork) add(n *Node) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[n.id] = n
}

func (m *memNetwork) isolate(id string, down bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.partition[id] = down
}

func (m *memNetwork) reachable(from, to string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.partition[from] && !m.partition[to]
}

func (m *memNetwork) transportFor(id string) Transport {
	return &memTransport{net: m, from: id}
}

type memTransport struct {
	net  *memNetwork
	from string
}

func (t *memTransport) SendRequestVote(target string, args *RequestVoteArgs) (*RequestVoteReply, error) {
	if !t.net.reachable(t.from, target) {
		return nil, errUnreachable
	}
	t.net.mu.RLock()
	n := t.net.nodes[target]
	t.net.mu.RUnlock()
	return n.HandleRequestVote(args), nil
}

func (t *memTransport) SendAppendEntries(target string, args *AppendEntriesArgs) (*AppendEntriesReply, error) {
	if !t.net.reachable(t.from, target) {
		return nil, errUnreachable
	}
	t.net.mu.RLock()
	n := t.net.nodes[target]
	t.net.mu.RUnlock()
	return n.HandleAppendEntries(args), nil
}

type unreachableErr struct{}

func (unreachableErr) Error() string { return "peer unreachable" }

var errUnreachable = unreachableErr{}

// cluster spins up n nodes wired through a shared in-memory network.
type cluster struct {
	net     *memNetwork
	nodes   []*Node
	applyCh map[string]chan ApplyMsg
}

func newCluster(t *testing.T, size int) *cluster {
	t.Helper()
	ids := make([]string, size)
	for i := range ids {
		ids[i] = string(rune('A' + i))
	}
	net := newMemNetwork()
	c := &cluster{net: net, applyCh: make(map[string]chan ApplyMsg)}
	cfg := Config{
		ElectionTimeoutMin: 150 * time.Millisecond,
		ElectionTimeoutMax: 300 * time.Millisecond,
		HeartbeatInterval:  50 * time.Millisecond,
	}
	for i, id := range ids {
		peers := make([]string, 0, size-1)
		for j, p := range ids {
			if i != j {
				peers = append(peers, p)
			}
		}
		ch := make(chan ApplyMsg, 256)
		c.applyCh[id] = ch
		n := NewNode(id, peers, cfg, net.transportFor(id), ch)
		net.add(n)
		c.nodes = append(c.nodes, n)
	}
	for _, n := range c.nodes {
		n.Start()
	}
	t.Cleanup(func() {
		for _, n := range c.nodes {
			n.Stop()
		}
	})
	return c
}

func (c *cluster) leader() *Node {
	for _, n := range c.nodes {
		if n.IsLeader() {
			return n
		}
	}
	return nil
}

func (c *cluster) waitLeader(t *testing.T, timeout time.Duration) *Node {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l := c.leader(); l != nil {
			// Confirm stability: leader remains for a short window.
			time.Sleep(100 * time.Millisecond)
			if l.IsLeader() {
				return l
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no stable leader elected within timeout")
	return nil
}

func TestLeaderElection(t *testing.T) {
	c := newCluster(t, 3)
	l := c.waitLeader(t, 3*time.Second)
	if l == nil {
		t.Fatal("expected a leader")
	}
	// Exactly one leader should exist.
	leaders := 0
	for _, n := range c.nodes {
		if n.IsLeader() {
			leaders++
		}
	}
	if leaders != 1 {
		t.Fatalf("expected exactly 1 leader, got %d", leaders)
	}
}

func TestLogReplicationAndCommit(t *testing.T) {
	c := newCluster(t, 3)
	l := c.waitLeader(t, 3*time.Second)

	idx, ok := l.Propose([]byte("set flag.dark_mode true"))
	if !ok {
		t.Fatal("propose rejected by leader")
	}

	// The command must be applied on a majority within a reasonable window.
	deadline := time.Now().Add(2 * time.Second)
	applied := 0
	for time.Now().Before(deadline) && applied < 2 {
		applied = 0
		for _, n := range c.nodes {
			if n.Status().LastApplied >= idx {
				applied++
			}
		}
		if applied >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if applied < 2 {
		t.Fatalf("entry not committed on a majority: applied on %d nodes", applied)
	}
}

func TestFailoverAfterLeaderCrash(t *testing.T) {
	c := newCluster(t, 3)
	old := c.waitLeader(t, 3*time.Second)

	// Isolate the current leader; the remaining two must elect a new one.
	c.net.isolate(old.id, true)

	deadline := time.Now().Add(3 * time.Second)
	var newLeader *Node
	for time.Now().Before(deadline) {
		for _, n := range c.nodes {
			if n.id != old.id && n.IsLeader() {
				newLeader = n
			}
		}
		if newLeader != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if newLeader == nil {
		t.Fatal("no new leader elected after leader crash")
	}
	if newLeader.id == old.id {
		t.Fatal("isolated node should not remain leader")
	}
}

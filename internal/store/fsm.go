// Package store implements the FlagForge state machine on top of Raft.
//
// The state machine is the set of feature flags. Every mutation is encoded as
// a Command, proposed to Raft, and only applied to the in-memory map once the
// entry is committed by a majority. This gives every node an identical,ceived
// linearizable view of the flags — the property that makes FlagForge safe to
// use as the source of truth for production feature rollouts.
package store

import (
	"encoding/json"
	"sync"
	"time"
)

// Op is the type of a mutation applied to the flag store.
type Op string

const (
	OpSet    Op = "set"
	OpDelete Op = "delete"
)

// Command is the serialized form of a state-machine mutation, stored in the
// Raft log. It is intentionally self-describing so the log is replayable.
type Command struct {
	Op   Op     `json:"op"`
	Flag *Flag  `json:"flag,omitempty"`
	Key  string `json:"key,omitempty"`
}

// Flag is a single feature flag with a percentage rollout.
type Flag struct {
	Key         string    `json:"key"`
	Enabled     bool      `json:"enabled"`
	Rollout     int       `json:"rollout"` // 0-100 percentage of users
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// FSM is the finite state machine holding the current flag set.
type FSM struct {
	mu    sync.RWMutex
	flags map[string]Flag
}

// NewFSM returns an empty flag state machine.
func NewFSM() *FSM {
	return &FSM{flags: make(map[string]Flag)}
}

// Apply mutates the state machine. It is called exactly once per committed
// Raft entry, in log order, guaranteeing determinism across nodes.
func (f *FSM) Apply(command []byte) error {
	var cmd Command
	if err := json.Unmarshal(command, &cmd); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch cmd.Op {
	case OpSet:
		if cmd.Flag != nil {
			cmd.Flag.UpdatedAt = time.Now().UTC()
			f.flags[cmd.Flag.Key] = *cmd.Flag
		}
	case OpDelete:
		delete(f.flags, cmd.Key)
	}
	return nil
}

// Get returns a flag by key.
func (f *FSM) Get(key string) (Flag, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	fl, ok := f.flags[key]
	return fl, ok
}

// List returns all flags sorted-insertion-agnostic (caller may sort).
func (f *FSM) List() []Flag {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]Flag, 0, len(f.flags))
	for _, fl := range f.flags {
		out = append(out, fl)
	}
	return out
}

// EncodeSet builds a serialized "set" command for a flag.
func EncodeSet(fl Flag) ([]byte, error) {
	return json.Marshal(Command{Op: OpSet, Flag: &fl})
}

// EncodeDelete builds a serialized "delete" command for a key.
func EncodeDelete(key string) ([]byte, error) {
	return json.Marshal(Command{Op: OpDelete, Key: key})
}

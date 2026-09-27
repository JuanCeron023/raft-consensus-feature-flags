// Package api exposes the public FlagForge HTTP API and a WebSocket stream
// that powers the live dashboard.
package api

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/portfolio/flagforge/internal/raft"
	"github.com/portfolio/flagforge/internal/store"
)

// Server ties together the Raft node, the flag FSM, and the HTTP surface.
type Server struct {
	node *raft.Node
	fsm  *store.FSM

	mu       sync.Mutex
	watchers map[chan []byte]struct{}
}

// New builds an API server and starts consuming committed entries.
func New(node *raft.Node, fsm *store.FSM, applyCh <-chan raft.ApplyMsg) *Server {
	s := &Server{
		node:     node,
		fsm:      fsm,
		watchers: make(map[chan []byte]struct{}),
	}
	go s.consume(applyCh)
	go s.broadcastStatus()
	return s
}

// consume applies every committed Raft entry to the state machine.
func (s *Server) consume(applyCh <-chan raft.ApplyMsg) {
	for msg := range applyCh {
		_ = s.fsm.Apply(msg.Command)
	}
}

// Routes registers the public endpoints on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/flags", s.handleFlags)
	mux.HandleFunc("/api/flags/", s.handleFlagByKey)
	mux.HandleFunc("/api/evaluate/", s.handleEvaluate)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/ws", s.handleWebSocket)
}

func (s *Server) handleFlags(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		flags := s.fsm.List()
		sort.Slice(flags, func(i, j int) bool { return flags[i].Key < flags[j].Key })
		writeJSON(w, http.StatusOK, flags)
	case http.MethodPost:
		var fl store.Flag
		if err := json.NewDecoder(r.Body).Decode(&fl); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if fl.Key == "" {
			writeErr(w, http.StatusBadRequest, errors.New("key is required"))
			return
		}
		if fl.Rollout < 0 || fl.Rollout > 100 {
			writeErr(w, http.StatusBadRequest, errors.New("rollout must be 0-100"))
			return
		}
		if err := s.propose(store.EncodeSet, fl); err != nil {
			writeErr(w, http.StatusServiceUnavailable, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFlagByKey(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Path[len("/api/flags/"):]
	if key == "" {
		writeErr(w, http.StatusBadRequest, errors.New("key required"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if fl, ok := s.fsm.Get(key); ok {
			writeJSON(w, http.StatusOK, fl)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	case http.MethodDelete:
		if !s.node.IsLeader() {
			writeLeaderHint(w, s.node)
			return
		}
		cmd, err := store.EncodeDelete(key)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if _, ok := s.node.Propose(cmd); !ok {
			writeLeaderHint(w, s.node)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleEvaluate returns whether a flag is on for a given user id, applying the
// deterministic percentage rollout so the same user always gets a stable answer.
func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Path[len("/api/evaluate/"):]
	user := r.URL.Query().Get("user")
	fl, ok := s.fsm.Get(key)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
		return
	}
	enabled := fl.Enabled && inRollout(user, fl.Key, fl.Rollout)
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
}

// inRollout deterministically buckets a user into 0-99 based on a stable hash
// of user+flag, so a 30% rollout consistently targets the same ~30% of users.
func inRollout(user, key string, rollout int) bool {
	if rollout >= 100 {
		return true
	}
	if rollout <= 0 {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(user + ":" + key))
	return int(h.Sum32()%100) < rollout
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.node.Status())
}

// propose is a helper that enforces leader-only writes and encodes a set.
func (s *Server) propose(enc func(store.Flag) ([]byte, error), fl store.Flag) error {
	if !s.node.IsLeader() {
		return errors.New("not leader; retry against current leader")
	}
	cmd, err := enc(fl)
	if err != nil {
		return err
	}
	if _, ok := s.node.Propose(cmd); !ok {
		return errors.New("not leader")
	}
	return nil
}

func writeLeaderHint(w http.ResponseWriter, node *raft.Node) {
	w.Header().Set("X-Leader-Hint", node.LeaderID())
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error":  "not leader",
		"leader": node.LeaderID(),
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// broadcastStatus pushes a status snapshot to all WebSocket watchers every
// 250ms so the dashboard can render live cluster state.
func (s *Server) broadcastStatus() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		snap := struct {
			Status raft.Status  `json:"status"`
			Flags  []store.Flag `json:"flags"`
		}{
			Status: s.node.Status(),
			Flags:  s.fsm.List(),
		}
		payload, _ := json.Marshal(snap)
		s.mu.Lock()
		for ch := range s.watchers {
			select {
			case ch <- payload:
			default:
			}
		}
		s.mu.Unlock()
	}
}

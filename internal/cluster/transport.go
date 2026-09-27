// Package cluster wires Raft nodes together over HTTP and connects the
// committed log to the FlagForge state machine.
package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/portfolio/flagforge/internal/raft"
)

// HTTPTransport implements raft.Transport over plain HTTP. Peer addresses are
// resolved via the addrs map (nodeID -> "host:port").
type HTTPTransport struct {
	self   string
	addrs  map[string]string
	client *http.Client
}

// NewHTTPTransport builds a transport for the given node.
func NewHTTPTransport(self string, addrs map[string]string) *HTTPTransport {
	return &HTTPTransport{
		self:  self,
		addrs: addrs,
		client: &http.Client{
			Timeout: 200 * time.Millisecond,
		},
	}
}

func (t *HTTPTransport) post(target, path string, in, out any) error {
	addr, ok := t.addrs[target]
	if !ok {
		return fmt.Errorf("unknown peer %q", target)
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s%s", addr, path)
	resp, err := t.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("peer %s returned %d", target, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// SendRequestVote implements raft.Transport.
func (t *HTTPTransport) SendRequestVote(target string, args *raft.RequestVoteArgs) (*raft.RequestVoteReply, error) {
	var reply raft.RequestVoteReply
	if err := t.post(target, "/raft/request-vote", args, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// SendAppendEntries implements raft.Transport.
func (t *HTTPTransport) SendAppendEntries(target string, args *raft.AppendEntriesArgs) (*raft.AppendEntriesReply, error) {
	var reply raft.AppendEntriesReply
	if err := t.post(target, "/raft/append-entries", args, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// RegisterHandlers mounts the internal Raft RPC endpoints on mux.
func RegisterHandlers(mux *http.ServeMux, node *raft.Node) {
	mux.HandleFunc("/raft/request-vote", func(w http.ResponseWriter, r *http.Request) {
		var args raft.RequestVoteArgs
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, node.HandleRequestVote(&args))
	})
	mux.HandleFunc("/raft/append-entries", func(w http.ResponseWriter, r *http.Request) {
		var args raft.AppendEntriesArgs
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, node.HandleAppendEntries(&args))
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

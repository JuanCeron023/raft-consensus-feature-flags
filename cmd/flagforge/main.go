// Command flagforge runs a single FlagForge node. A cluster is formed by
// starting several nodes with the same --peers set but distinct --id/--http.
//
// Example (3-node local cluster):
//
//	flagforge --id A --http :9001 --raft :7001 \
//	  --peers "A=localhost:7001,B=localhost:7002,C=localhost:7003"
//
// FlagForge is a self-hosted, strongly-consistent feature-flag service. Writes
// go through a from-scratch Raft implementation so flag changes are safe even
// while nodes crash or restart — a low-cost alternative to hosted flag SaaS
// that deploys as a single container on AWS ECS/Fargate.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/portfolio/flagforge/internal/api"
	"github.com/portfolio/flagforge/internal/cluster"
	"github.com/portfolio/flagforge/internal/raft"
	"github.com/portfolio/flagforge/internal/store"
)

func main() {
	var (
		id       = flag.String("id", "A", "unique node id")
		httpAddr = flag.String("http", ":9001", "public HTTP/API/dashboard listen address")
		raftAddr = flag.String("raft", "", "raft transport listen address (overrides the address from --peers)")
		peersRaw = flag.String("peers", "A=localhost:7001", "comma list of id=host:port for raft transport")
	)
	flag.Parse()

	addrs := parsePeers(*peersRaw)
	selfRaft, ok := addrs[*id]
	if !ok {
		log.Fatalf("node id %q not found in --peers", *id)
	}
	// The Raft transport binds to all interfaces on the peer's port; --raft
	// lets an operator override the bind address (e.g. inside containers).
	if *raftAddr != "" {
		selfRaft = *raftAddr
	}

	peerIDs := make([]string, 0, len(addrs)-1)
	for pid := range addrs {
		if pid != *id {
			peerIDs = append(peerIDs, pid)
		}
	}

	applyCh := make(chan raft.ApplyMsg, 1024)
	tr := cluster.NewHTTPTransport(*id, addrs)
	node := raft.NewNode(*id, peerIDs, raft.DefaultConfig(), tr, applyCh)

	fsm := store.NewFSM()
	server := api.New(node, fsm, applyCh)

	// Internal Raft transport listens on its own address.
	raftMux := http.NewServeMux()
	cluster.RegisterHandlers(raftMux, node)
	raftSrv := &http.Server{Addr: selfRaft, Handler: raftMux}
	go func() {
		log.Printf("[%s] raft transport on %s", *id, selfRaft)
		if err := raftSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("raft server: %v", err)
		}
	}()

	// Public HTTP: API + WebSocket + static dashboard.
	pubMux := http.NewServeMux()
	server.Routes(pubMux)
	pubMux.Handle("/", http.FileServer(http.Dir("web/static")))
	pubSrv := &http.Server{Addr: *httpAddr, Handler: withCORS(pubMux)}

	node.Start()
	go func() {
		log.Printf("[%s] dashboard + API on http://localhost%s", *id, *httpAddr)
		if err := pubSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	// Graceful shutdown.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("[%s] shutting down", *id)
	node.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = pubSrv.Shutdown(ctx)
	_ = raftSrv.Shutdown(ctx)
}

func parsePeers(raw string) map[string]string {
	addrs := make(map[string]string)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			addrs[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return addrs
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

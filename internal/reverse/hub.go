package reverse

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shui1iao/DockUP/internal/agent"
	"github.com/shui1iao/DockUP/internal/config"
)

type Hub struct {
	store *config.Store
	log   *slog.Logger
	mu    sync.Mutex
	conns map[string]*agentConn
}

type agentConn struct {
	server  config.StoredAgent
	enc     *json.Encoder
	flusher http.Flusher
	jobs    map[string]chan envelope
	mu      sync.Mutex
}

type envelope struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	Path  string          `json:"path,omitempty"`
	Body  json.RawMessage `json:"body,omitempty"`
	Error string          `json:"error,omitempty"`
}

func NewHub(store *config.Store, log *slog.Logger) *Hub {
	return &Hub{store: store, log: log, conns: map[string]*agentConn{}}
}

func (h *Hub) Handle(w http.ResponseWriter, r *http.Request) {
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.EnableFullDuplex()
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	name := strings.TrimSpace(r.Header.Get("X-DockUP-Name"))
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !h.tokenKnown(token) {
		http.Error(w, "pair first", http.StatusUnauthorized)
		return
	}
	server, _, err := h.store.AddOrUpdateReverseServer(name, token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	flusher, _ := w.(http.Flusher)
	conn := &agentConn{server: server, enc: json.NewEncoder(w), flusher: flusher, jobs: map[string]chan envelope{}}
	h.mu.Lock()
	h.conns[server.ID] = conn
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if h.conns[server.ID] == conn {
			delete(h.conns, server.ID)
		}
		h.mu.Unlock()
	}()
	_ = conn.enc.Encode(envelope{Type: "hello", ID: server.ID})
	if h.log != nil {
		h.log.Info("reverse agent connected", "server", server.Name, "id", server.ID)
	}
	if flusher != nil {
		flusher.Flush()
	}
	dec := json.NewDecoder(r.Body)
	for {
		var msg envelope
		if err := dec.Decode(&msg); err != nil {
			if err != io.EOF && h.log != nil {
				h.log.Warn("reverse agent disconnected", "server", server.Name, "error", err)
			}
			return
		}
		if msg.Type == "pong" {
			_, _, _ = h.store.AddOrUpdateReverseServer(server.Name, token)
			continue
		}
		if msg.ID != "" {
			conn.mu.Lock()
			ch := conn.jobs[msg.ID]
			conn.mu.Unlock()
			if ch != nil {
				select {
				case ch <- msg:
				default:
				}
			}
		}
	}
}

func (h *Hub) tokenKnown(token string) bool {
	for _, s := range h.store.Servers() {
		if subtle.ConstantTimeCompare([]byte(s.Token), []byte(token)) == 1 {
			return true
		}
	}
	for _, p := range h.store.PendingPairs() {
		if subtle.ConstantTimeCompare([]byte(p.Token), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

func (h *Hub) Request(ctx context.Context, id, path string, body any) (json.RawMessage, error) {
	h.mu.Lock()
	conn := h.conns[id]
	h.mu.Unlock()
	if conn == nil {
		return nil, fmt.Errorf("agent offline")
	}
	var raw json.RawMessage
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	jobID := fmt.Sprintf("%d", time.Now().UnixNano())
	ch := make(chan envelope, 1)
	conn.mu.Lock()
	conn.jobs[jobID] = ch
	err := conn.enc.Encode(envelope{Type: "request", ID: jobID, Path: path, Body: raw})
	if err == nil && conn.flusher != nil {
		conn.flusher.Flush()
	}
	conn.mu.Unlock()
	if h.log != nil && err == nil {
		h.log.Info("reverse request sent", "server", conn.server.Name, "path", path)
	}
	if err != nil {
		return nil, err
	}
	defer func() { conn.mu.Lock(); delete(conn.jobs, jobID); conn.mu.Unlock() }()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != "" {
			return nil, fmt.Errorf("%s", resp.Error)
		}
		return resp.Body, nil
	}
}

func (h *Hub) OnlineIDs() map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := map[string]bool{}
	for id := range h.conns {
		m[id] = true
	}
	return m
}

func (h *Hub) IsOnline(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[id] != nil
}

var _ = agent.Snapshot{}

package agent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/shuijiao1/DockUP/internal/dockerx"
)

type Server struct {
	docker *dockerx.Client
	token  string
	name   string
	log    *slog.Logger
	srv    *http.Server
}

type Snapshot struct {
	Name     string            `json:"name"`
	Projects []ProjectSnapshot `json:"projects"`
	Totals   Totals            `json:"totals"`
	Time     time.Time         `json:"time"`
}

type Totals struct {
	Projects   int `json:"projects"`
	Containers int `json:"containers"`
	Running    int `json:"running"`
}

type ProjectSnapshot struct {
	Key        string                    `json:"key"`
	Name       string                    `json:"name"`
	Type       string                    `json:"type"`
	WorkingDir string                    `json:"working_dir,omitempty"`
	ConfigFile string                    `json:"config_file,omitempty"`
	Containers []dockerx.ContainerDetail `json:"containers"`
}

type UpdateInfo struct {
	Container  dockerx.ContainerInfo `json:"container"`
	OldVersion dockerx.ImageVersion  `json:"old_version"`
	NewVersion dockerx.ImageVersion  `json:"new_version"`
}

type updateContainerReq struct {
	Image   string `json:"image"`
	Cleanup bool   `json:"cleanup"`
}

func NewServer(docker *dockerx.Client, token, name string, log *slog.Logger) *Server {
	return &Server{docker: docker, token: strings.TrimSpace(token), name: strings.TrimSpace(name), log: log}
}

func (s *Server) Start(ctx context.Context, listen string) error {
	if s.token == "" {
		return fmt.Errorf("DOCKUP_AGENT_TOKEN is required in agent mode")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/snapshot", s.auth(s.handleSnapshot))
	mux.HandleFunc("/v1/pair/complete", s.auth(s.handlePairComplete))
	mux.HandleFunc("/v1/updates/check", s.auth(s.handleCheckUpdates))
	mux.HandleFunc("/v1/containers/", s.auth(s.handleContainerUpdate))
	mux.HandleFunc("/v1/projects/", s.auth(s.handleProjectAction))

	s.srv = &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
	}()
	s.log.Info("DockUP agent listening", "listen", listen)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "time": time.Now()})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap, err := s.snapshot(r.Context(), s.name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, snap)
}

func (s *Server) handlePairComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "name": s.name, "time": time.Now()})
}

func (s *Server) handleCheckUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Time{})
	}
	updates, err := s.checkUpdates(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, updates)
}

func (s *Server) handleContainerUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/containers/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "update" {
		http.Error(w, "invalid container update path", http.StatusBadRequest)
		return
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil {
		http.Error(w, "invalid container id", http.StatusBadRequest)
		return
	}
	var req updateContainerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Image) == "" {
		http.Error(w, "image is required", http.StatusBadRequest)
		return
	}
	if isCurrentContainerID(id) {
		writeJSON(w, map[string]any{"ok": true, "self_update": true, "queued": true})
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			time.Sleep(500 * time.Millisecond)
			if _, err := s.docker.RunSelfUpdateHelper(ctx, req.Image, id, req.Image, req.Cleanup); err != nil {
				s.log.Warn("remote self update helper failed", "container", id, "image", req.Image, "error", err)
			}
		}()
		return
	}
	oldID, newID, err := s.docker.UpdateContainer(r.Context(), id, req.Image, req.Cleanup)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "old_id": oldID, "new_id": newID})
}

func isCurrentContainerID(id string) bool {
	hostname, err := os.Hostname()
	if err != nil {
		return false
	}
	id = normalizeID(id)
	hostname = normalizeID(hostname)
	return id != "" && hostname != "" && strings.HasPrefix(id, hostname)
}

func (s *Server) handleProjectAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/projects/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		http.Error(w, "invalid project action path", http.StatusBadRequest)
		return
	}
	key, action := parts[0], parts[1]
	p, err := s.docker.Project(r.Context(), key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	for _, c := range p.Containers {
		switch action {
		case "start":
			err = s.docker.StartContainer(r.Context(), c.ID)
		case "stop":
			err = s.docker.StopContainer(r.Context(), c.ID)
		case "restart":
			err = s.docker.RestartContainer(r.Context(), c.ID)
		default:
			http.Error(w, "unsupported action", http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]any{"ok": true, "project": key, "action": action})
}

func (s *Server) checkUpdates(ctx context.Context) ([]UpdateInfo, error) {
	containers, err := s.docker.RunningContainers(ctx)
	if err != nil {
		return nil, err
	}
	updates := []UpdateInfo{}
	for _, c := range containers {
		oldVersion, err := s.docker.InspectImageVersionByIDWithRef(ctx, c.ImageID, c.Image)
		if err != nil {
			s.log.Warn("remote update check inspect failed", "container", c.Name, "image", c.Image, "error", err)
			continue
		}
		if oldVersion.ID == "" {
			oldVersion.ID = c.ImageID
		}
		if err := s.docker.PullImage(ctx, c.Image); err != nil {
			s.log.Warn("remote update check pull failed", "container", c.Name, "image", c.Image, "error", err)
			continue
		}
		newVersion, err := s.docker.InspectImageVersion(ctx, c.Image)
		if err != nil {
			s.log.Warn("remote update check inspect new failed", "container", c.Name, "image", c.Image, "error", err)
			continue
		}
		if !dockerx.SameEffectiveImage(oldVersion, newVersion) {
			updates = append(updates, UpdateInfo{Container: c, OldVersion: oldVersion, NewVersion: newVersion})
		}
	}
	return updates, nil
}

func normalizeID(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "sha256:")
}

func (s *Server) snapshot(ctx context.Context, name string) (Snapshot, error) {
	projects, err := s.docker.Projects(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Name: name, Time: time.Now(), Projects: []ProjectSnapshot{}}
	for _, p := range projects {
		ps := ProjectSnapshot{Key: p.Key, Name: p.Name, Type: p.Type, WorkingDir: p.WorkingDir, ConfigFile: p.ConfigFile}
		for _, c := range p.Containers {
			d, err := s.docker.ContainerDetail(ctx, c.ID)
			if err != nil {
				continue
			}
			ps.Containers = append(ps.Containers, d)
			snap.Totals.Containers++
			if d.State == "running" {
				snap.Totals.Running++
			}
		}
		snap.Projects = append(snap.Projects, ps)
	}
	snap.Totals.Projects = len(snap.Projects)
	return snap, nil
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/snapshot", s.auth(s.handleSnapshot))
	mux.HandleFunc("/v1/pair/complete", s.auth(s.handlePairComplete))
	mux.HandleFunc("/v1/updates/check", s.auth(s.handleCheckUpdates))
	mux.HandleFunc("/v1/containers/", s.auth(s.handleContainerUpdate))
	mux.HandleFunc("/v1/projects/", s.auth(s.handleProjectAction))
	mux.ServeHTTP(w, r)
}

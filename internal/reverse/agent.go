package reverse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shui1iao/DockUP/internal/agent"
	"github.com/shui1iao/DockUP/internal/dockerx"
)

type Agent struct {
	docker  *dockerx.Client
	url     string
	token   string
	name    string
	log     *slog.Logger
	timeout time.Duration
}

func NewAgent(docker *dockerx.Client, url, token, name string, log *slog.Logger, timeout ...time.Duration) *Agent {
	t := 20 * time.Second
	if len(timeout) > 0 && timeout[0] > 0 {
		t = timeout[0]
	}
	return &Agent{docker: docker, url: strings.TrimRight(strings.TrimSpace(url), "/"), token: strings.TrimSpace(token), name: strings.TrimSpace(name), log: log, timeout: t}
}

func (a *Agent) Run(ctx context.Context) error {
	if a.url == "" || a.token == "" {
		return fmt.Errorf("DOCKUP_PUBLIC_URL and DOCKUP_AGENT_TOKEN are required")
	}
	for {
		if err := a.connect(ctx); err != nil && ctx.Err() == nil && a.log != nil {
			a.log.Warn("reverse connect failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (a *Agent) connect(ctx context.Context) error {
	pr, pw := io.Pipe()
	go func() {
		<-ctx.Done()
		_ = pr.CloseWithError(ctx.Err())
		_ = pw.CloseWithError(ctx.Err())
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url+"/v1/reverse/connect", pr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.Header.Set("X-DockUP-Name", a.name)
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		_ = pw.Close()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = pw.Close()
		return fmt.Errorf("connect failed: %s", resp.Status)
	}
	enc := json.NewEncoder(pw)
	var writeMu sync.Mutex
	write := func(msg envelope) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return enc.Encode(msg)
	}
	dec := json.NewDecoder(resp.Body)
	defer pw.Close()
	for {
		var msg envelope
		if err := dec.Decode(&msg); err != nil {
			return err
		}
		switch msg.Type {
		case "hello":
			if a.log != nil {
				a.log.Info("reverse agent connected", "id", msg.ID)
			}
		case "ping":
			_ = write(envelope{Type: "pong", ID: msg.ID})
		case "request":
			msg := msg
			go func() {
				reqCtx, cancel := context.WithTimeout(ctx, a.timeout)
				defer cancel()
				body, errText := a.handle(reqCtx, msg.Path, msg.Body)
				resp := envelope{Type: "response", ID: msg.ID, Body: body}
				if errText != "" {
					resp.Error = errText
				}
				if err := write(resp); err != nil && a.log != nil {
					a.log.Warn("reverse response failed", "path", msg.Path, "error", err)
				}
			}()
		}
	}
}

func (a *Agent) handle(ctx context.Context, path string, body json.RawMessage) (json.RawMessage, string) {
	s := agent.NewServer(a.docker, a.token, a.name, a.log)
	w := &memoryResponse{header: http.Header{}}
	method := http.MethodGet
	if path == "/v1/updates/check" || strings.Contains(path, "/update") || strings.Contains(path, "/projects/") {
		method = http.MethodPost
	}
	req, _ := http.NewRequestWithContext(ctx, method, "http://agent"+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+a.token)
	s.ServeHTTP(w, req)
	if w.status >= 400 {
		return nil, strings.TrimSpace(w.buf.String())
	}
	return w.buf.Bytes(), ""
}

type memoryResponse struct {
	header http.Header
	buf    bytes.Buffer
	status int
}

func (m *memoryResponse) Header() http.Header { return m.header }
func (m *memoryResponse) Write(b []byte) (int, error) {
	if m.status == 0 {
		m.status = 200
	}
	return m.buf.Write(b)
}
func (m *memoryResponse) WriteHeader(code int) { m.status = code }

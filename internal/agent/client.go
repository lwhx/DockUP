package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shui1iao/DockUP/internal/config"
)

type Client struct {
	agents []config.AgentConfig
	token  string
	http   *http.Client
}

type RemoteSnapshot struct {
	Agent config.AgentConfig
	Data  Snapshot
	Err   error
}

func NewClient(agents []config.AgentConfig, token string) *Client {
	return &Client{agents: agents, token: strings.TrimSpace(token), http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) SetAgents(agents []config.AgentConfig) {
	c.agents = agents
}

func (c *Client) Enabled() bool {
	if c == nil || len(c.agents) == 0 {
		return false
	}
	if c.token != "" {
		return true
	}
	for _, a := range c.agents {
		if a.Token != "" {
			return true
		}
	}
	return false
}

func (c *Client) Agents() []config.AgentConfig { return append([]config.AgentConfig(nil), c.agents...) }

func (c *Client) Snapshots(ctx context.Context) []RemoteSnapshot {
	out := make([]RemoteSnapshot, 0, len(c.agents))
	for _, a := range c.agents {
		var snap Snapshot
		err := c.get(ctx, a.URL+"/v1/snapshot", &snap)
		if snap.Name == "" {
			snap.Name = a.Name
		}
		out = append(out, RemoteSnapshot{Agent: a, Data: snap, Err: err})
	}
	return out
}

func (c *Client) Snapshot(ctx context.Context, id string) (config.AgentConfig, Snapshot, error) {
	a, ok := c.find(id)
	if !ok {
		return config.AgentConfig{}, Snapshot{}, fmt.Errorf("agent not found")
	}
	var snap Snapshot
	err := c.get(ctx, a.URL+"/v1/snapshot", &snap)
	if snap.Name == "" {
		snap.Name = a.Name
	}
	return a, snap, err
}

func (c *Client) ProjectAction(ctx context.Context, id, key, action string) error {
	a, ok := c.find(id)
	if !ok {
		return fmt.Errorf("agent not found")
	}
	return c.post(ctx, a.URL+"/v1/projects/"+key+"/"+action, nil, nil)
}

func (c *Client) CheckUpdates(ctx context.Context, id string) (config.AgentConfig, []UpdateInfo, error) {
	a, ok := c.find(id)
	if !ok {
		return config.AgentConfig{}, nil, fmt.Errorf("agent not found")
	}
	var updates []UpdateInfo
	err := c.post(ctx, a.URL+"/v1/updates/check", nil, &updates)
	return a, updates, err
}

func (c *Client) PairComplete(ctx context.Context, agent config.AgentConfig, pairID, token string) error {
	oldAgents := c.agents
	c.agents = []config.AgentConfig{agent}
	defer func() { c.agents = oldAgents }()
	body := PairRequest{PairID: pairID, Token: token}
	return c.post(ctx, agent.URL+"/v1/pair/complete", body, nil)
}

func (c *Client) UpdateContainer(ctx context.Context, id, containerID, image string, cleanup bool) error {
	a, ok := c.find(id)
	if !ok {
		return fmt.Errorf("agent not found")
	}
	body := map[string]any{"image": image, "cleanup": cleanup}
	return c.post(ctx, a.URL+"/v1/containers/"+containerID+"/update", body, nil)
}

func (c *Client) find(id string) (config.AgentConfig, bool) {
	for _, a := range c.agents {
		if a.ID == id {
			return a, true
		}
	}
	return config.AgentConfig{}, false
}

func (c *Client) get(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, url string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	token := c.token
	if token == "" {
		for _, a := range c.agents {
			if strings.HasPrefix(req.URL.String(), a.URL) && a.Token != "" {
				token = a.Token
				break
			}
		}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("agent API %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

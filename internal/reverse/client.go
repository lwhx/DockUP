package reverse

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shui1iao/DockUP/internal/agent"
	"github.com/shui1iao/DockUP/internal/config"
)

type Client struct{ hub *Hub }

func NewClient(hub *Hub) *Client { return &Client{hub: hub} }
func (c *Client) Enabled() bool  { return c != nil && c.hub != nil }
func (c *Client) OnlineIDs() map[string]bool {
	if c == nil || c.hub == nil {
		return map[string]bool{}
	}
	return c.hub.OnlineIDs()
}

func (c *Client) Snapshot(ctx context.Context, a config.AgentConfig) (agent.Snapshot, error) {
	var snap agent.Snapshot
	b, err := c.hub.Request(ctx, a.ID, "/v1/snapshot", nil)
	if err != nil {
		return snap, err
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return snap, err
	}
	if snap.Name == "" {
		snap.Name = a.Name
	}
	return snap, nil
}

func (c *Client) CheckUpdates(ctx context.Context, a config.AgentConfig) ([]agent.UpdateInfo, error) {
	var updates []agent.UpdateInfo
	b, err := c.hub.Request(ctx, a.ID, "/v1/updates/check", nil)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return updates, nil
	}
	return updates, json.Unmarshal(b, &updates)
}

func (c *Client) ProjectAction(ctx context.Context, a config.AgentConfig, key, action string) error {
	_, err := c.hub.Request(ctx, a.ID, fmt.Sprintf("/v1/projects/%s/%s", key, action), nil)
	return err
}

func (c *Client) IsOnline(id string) bool {
	if c == nil || c.hub == nil {
		return false
	}
	return c.hub.IsOnline(id)
}

func (c *Client) UpdateContainer(ctx context.Context, a config.AgentConfig, containerID, image string, cleanup bool) error {
	_, err := c.hub.Request(ctx, a.ID, fmt.Sprintf("/v1/containers/%s/update", containerID), map[string]any{"image": image, "cleanup": cleanup})
	return err
}

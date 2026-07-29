package dockerx

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type ProjectInfo struct {
	Key        string
	Name       string
	Type       string
	WorkingDir string
	ConfigFile string
	Containers []ContainerInfo
}

type ContainerDetail struct {
	Info       ContainerInfo
	State      string
	Status     string
	Health     string
	Created    string
	Started    string
	Restarting bool
	Compose    bool
	Project    string
	Service    string
	WorkingDir string
	ConfigFile string
	Ports      string
	Restarts   int64
	CPUPercent float64
	Memory     uint64
	MemoryMax  uint64
	NetRx      uint64
	NetTx      uint64
	BlockRead  uint64
	BlockWrite uint64
	Version    ImageVersion
}

func (c *Client) AllContainers(ctx context.Context) ([]ContainerInfo, error) {
	var items []listItem
	if err := c.doJSON(ctx, http.MethodGet, "/containers/json?all=true", nil, &items); err != nil {
		return nil, err
	}
	out := make([]ContainerInfo, 0, len(items))
	for _, item := range items {
		out = append(out, c.containerInfoFromListItem(ctx, item))
	}
	return out, nil
}

func (c *Client) Projects(ctx context.Context) ([]ProjectInfo, error) {
	var items []listItem
	if err := c.doJSON(ctx, http.MethodGet, "/containers/json?all=true", nil, &items); err != nil {
		return nil, err
	}
	projects := map[string]*ProjectInfo{}
	for _, item := range items {
		ci := c.containerInfoFromListItem(ctx, item)
		key := ci.ID[:12]
		name := ci.Name
		typ := "docker"
		workingDir := ""
		configFile := ""
		if project := item.Labels["com.docker.compose.project"]; project != "" {
			key = "compose:" + project
			name = project
			typ = "compose"
			workingDir = item.Labels["com.docker.compose.project.working_dir"]
			configFile = item.Labels["com.docker.compose.project.config_files"]
		}
		p := projects[key]
		if p == nil {
			p = &ProjectInfo{Key: key, Name: name, Type: typ, WorkingDir: workingDir, ConfigFile: configFile}
			projects[key] = p
		}
		p.Containers = append(p.Containers, ci)
	}
	out := make([]ProjectInfo, 0, len(projects))
	for _, p := range projects {
		sort.Slice(p.Containers, func(i, j int) bool { return p.Containers[i].Name < p.Containers[j].Name })
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type == "compose"
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (c *Client) Project(ctx context.Context, key string) (ProjectInfo, error) {
	projects, err := c.Projects(ctx)
	if err != nil {
		return ProjectInfo{}, err
	}
	for _, p := range projects {
		if p.Key == key {
			return p, nil
		}
	}
	return ProjectInfo{}, fmt.Errorf("project not found")
}

func (c *Client) ContainerDetail(ctx context.Context, id string) (ContainerDetail, error) {
	var inspect map[string]any
	if err := c.doJSON(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/json", nil, &inspect); err != nil {
		return ContainerDetail{}, err
	}
	cfg, _ := inspect["Config"].(map[string]any)
	labels, _ := cfg["Labels"].(map[string]any)
	state, _ := inspect["State"].(map[string]any)
	network, _ := inspect["NetworkSettings"].(map[string]any)
	name := strings.TrimPrefix(str(inspect["Name"]), "/")
	image := str(cfg["Image"])
	if image == "" {
		image = str(inspect["Image"])
	}
	d := ContainerDetail{
		Info: ContainerInfo{
			ID:      str(inspect["Id"]),
			Name:    name,
			Image:   image,
			ImageID: str(inspect["Image"]),
		},
		State:      str(state["Status"]),
		Status:     str(state["Status"]),
		Restarting: boolVal(state["Restarting"]),
		Restarts:   intVal(inspect["RestartCount"]),
		Created:    formatTimeShort(str(inspect["Created"])),
		Started:    formatTimeShort(str(state["StartedAt"])),
		Project:    label(labels, "com.docker.compose.project"),
		Service:    label(labels, "com.docker.compose.service"),
		WorkingDir: label(labels, "com.docker.compose.project.working_dir"),
		ConfigFile: label(labels, "com.docker.compose.project.config_files"),
		Ports:      formatPorts(network["Ports"]),
	}
	d.Compose = d.Project != ""
	if health, _ := state["Health"].(map[string]any); health != nil {
		d.Health = str(health["Status"])
	}
	versionCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	if v, err := c.InspectLocalImageVersionByIDWithRef(versionCtx, d.Info.ImageID, d.Info.Image); err == nil {
		d.Version = v
	}
	cancel()
	if d.Version.Tag == "" {
		bundledCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		c.EnrichBundledAppVersion(bundledCtx, &d.Version, d.Info.Image)
		cancel()
	}
	return d, nil
}

func shouldInspectImageVersion(image string) bool {
	image = strings.TrimSpace(strings.ToLower(image))
	if image == "" || strings.HasPrefix(image, "sha256:") || strings.Contains(image, "@sha256:") {
		return false
	}
	return strings.HasPrefix(image, "ghcr.io/shui1iao/") ||
		strings.HasPrefix(image, "xream/sub-store") ||
		strings.HasPrefix(image, "kwxos/tgbot-rss")
}

func (c *Client) fillStats(ctx context.Context, d *ContainerDetail) error {
	if d.Info.ID == "" || d.State != "running" {
		return nil
	}
	var stats map[string]any
	path := "/containers/" + url.PathEscape(d.Info.ID) + "/stats?stream=false&one-shot=true"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &stats); err != nil {
		return err
	}
	cpu, _ := stats["cpu_stats"].(map[string]any)
	pre, _ := stats["precpu_stats"].(map[string]any)
	d.CPUPercent = cpuPercent(cpu, pre)
	mem, _ := stats["memory_stats"].(map[string]any)
	d.Memory = uintVal(mem["usage"])
	d.MemoryMax = uintVal(mem["limit"])
	nets, _ := stats["networks"].(map[string]any)
	for _, raw := range nets {
		m, _ := raw.(map[string]any)
		d.NetRx += uintVal(m["rx_bytes"])
		d.NetTx += uintVal(m["tx_bytes"])
	}
	blk, _ := stats["blkio_stats"].(map[string]any)
	entries, _ := blk["io_service_bytes_recursive"].([]any)
	for _, raw := range entries {
		m, _ := raw.(map[string]any)
		switch strings.ToLower(str(m["op"])) {
		case "read":
			d.BlockRead += uintVal(m["value"])
		case "write":
			d.BlockWrite += uintVal(m["value"])
		}
	}
	return nil
}

func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.postEmpty(ctx, "/containers/"+url.PathEscape(id)+"/start")
}

func (c *Client) StopContainer(ctx context.Context, id string) error {
	return c.postEmpty(ctx, "/containers/"+url.PathEscape(id)+"/stop?t=30")
}

func (c *Client) RestartContainer(ctx context.Context, id string) error {
	return c.postEmpty(ctx, "/containers/"+url.PathEscape(id)+"/restart?t=30")
}

func (c *Client) DeleteContainer(ctx context.Context, id string) error {
	return c.delete(ctx, "/containers/"+url.PathEscape(id)+"?force=true&v=false")
}

func (c *Client) containerInfoFromListItem(ctx context.Context, item listItem) ContainerInfo {
	ci := containerInfoFromListItem(item)
	if !isPullable(ci.Image) {
		if image := c.resolveContainerImageRef(ctx, item.ID); image != "" {
			ci.Image = image
		}
	}
	return ci
}

func containerInfoFromListItem(item listItem) ContainerInfo {
	name := item.ID[:12]
	if len(item.Names) > 0 {
		name = strings.TrimPrefix(item.Names[0], "/")
	}
	return ContainerInfo{ID: item.ID, Name: name, Image: item.Image, ImageID: item.ImageID, State: item.State}
}

func formatPorts(raw any) string {
	ports, _ := raw.(map[string]any)
	if len(ports) == 0 {
		return "-"
	}
	parts := []string{}
	for private, bindsRaw := range ports {
		binds, _ := bindsRaw.([]any)
		if len(binds) == 0 {
			parts = append(parts, private)
			continue
		}
		for _, b := range binds {
			m, _ := b.(map[string]any)
			hostIP := str(m["HostIp"])
			hostPort := str(m["HostPort"])
			if hostIP == "" || hostIP == "0.0.0.0" {
				hostIP = "*"
			}
			parts = append(parts, fmt.Sprintf("%s:%s→%s", hostIP, hostPort, private))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func label(labels map[string]any, key string) string { return str(labels[key]) }

func boolVal(v any) bool {
	b, _ := v.(bool)
	return b
}

func intVal(v any) int64 { return int64(numVal(v)) }

func uintVal(v any) uint64 {
	n := numVal(v)
	if n < 0 {
		return 0
	}
	return uint64(n)
}

func numVal(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case int:
		return float64(x)
	default:
		return 0
	}
}

func cpuPercent(cpu, pre map[string]any) float64 {
	cpuUsage, _ := cpu["cpu_usage"].(map[string]any)
	preUsage, _ := pre["cpu_usage"].(map[string]any)
	cpuDelta := numVal(cpuUsage["total_usage"]) - numVal(preUsage["total_usage"])
	systemDelta := numVal(cpu["system_cpu_usage"]) - numVal(pre["system_cpu_usage"])
	online := numVal(cpu["online_cpus"])
	if online <= 0 {
		percpu, _ := cpuUsage["percpu_usage"].([]any)
		online = float64(len(percpu))
	}
	if cpuDelta <= 0 || systemDelta <= 0 || online <= 0 {
		return 0
	}
	return math.Round((cpuDelta/systemDelta)*online*100*100) / 100
}

func FormatBytes(n uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d%s", n, units[i])
	}
	return fmt.Sprintf("%.1f%s", f, units[i])
}

func formatTimeShort(t string) string {
	parsed, err := time.Parse(time.RFC3339Nano, t)
	if err != nil || parsed.IsZero() {
		return "-"
	}
	return parsed.Local().Format("2006-01-02 15:04")
}

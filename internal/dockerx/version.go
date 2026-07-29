package dockerx

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

var semverTagRE = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
var hexRE = regexp.MustCompile(`^[0-9a-fA-F]+$`)
var subStoreBundleVersionRE = regexp.MustCompile(`SUB_STORE(?:_BACKEND)?_VERSION:\s*(v?[0-9]+\.[0-9]+\.[0-9]+)`)
var tgbotRSSVersionRE = regexp.MustCompile(`main\.version=(v?[0-9]+\.[0-9]+\.[0-9]+)`)
var packageJSONVersionRE = regexp.MustCompile(`"version"\s*:\s*"(v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?)"`)
var gukoVersionRE = regexp.MustCompile(`GUKO_VERSION[^\n]*['"](v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?)['"]`)

type ImageVersion struct {
	Ref       string
	ID        string
	Digest    string
	Tag       string
	RemoteTag string
	Revision  string
}

func (v ImageVersion) Display() string {
	id := shortID(v.ID)
	tag := strings.TrimSpace(v.Tag)
	if tag == "" || isBareDigestTag(tag) {
		tag = strings.TrimSpace(v.RemoteTag)
	}
	if tag == "" || isBareDigestTag(tag) {
		return id
	}
	if id == "" {
		return tag
	}
	return fmt.Sprintf("%s (%s)", tag, id)
}

func isBareDigestTag(tag string) bool {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "sha256:")
	return len(tag) >= 32 && hexRE.MatchString(tag)
}

func (v ImageVersion) SameTag(other ImageVersion) bool {
	return v.Tag != "" && v.Tag == other.Tag
}

// SameEffectiveImage reports whether two image inspections point to the same
// runnable image, or to builds of the same application source revision where at
// least one side is an unversioned/floating build.  This avoids false positives
// for registries that publish both a semver tag and a separate latest/main image
// from the same commit, while still reporting normal digest changes for pinned
// semver tags.
func SameEffectiveImage(a, b ImageVersion) bool {
	if normalizeImageID(a.ID) != "" && normalizeImageID(a.ID) == normalizeImageID(b.ID) {
		return true
	}
	if strings.TrimSpace(a.Revision) == "" || strings.TrimSpace(b.Revision) == "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(a.Revision), strings.TrimSpace(b.Revision)) {
		return false
	}
	if releaseTag(a) == "" || releaseTag(b) == "" {
		return true
	}
	return releaseTag(a) == releaseTag(b) && (isFloatingRef(a.Ref) || isFloatingRef(b.Ref))
}

func (c *Client) InspectImageVersion(ctx context.Context, ref string) (ImageVersion, error) {
	var data map[string]any
	if err := c.doJSON(ctx, http.MethodGet, "/images/"+url.PathEscape(ref)+"/json", nil, &data); err != nil {
		return ImageVersion{}, err
	}

	v := ImageVersion{Ref: ref}
	if id, _ := data["Id"].(string); id != "" {
		v.ID = id
	} else if id, _ := data["ID"].(string); id != "" {
		v.ID = id
	}
	v.Revision = imageRevisionLabel(data)

	if digests, _ := data["RepoDigests"].([]any); len(digests) > 0 {
		for _, raw := range digests {
			d := str(raw)
			if strings.Contains(d, "@sha256:") {
				v.Digest = d
				break
			}
		}
	}
	if v.Digest == "" {
		v.Digest = digestFromRef(ref)
	}

	if tag := tagFromRef(ref); tag != "" && tag != "latest" && !isBareDigestTag(tag) {
		v.Tag = tag
	}
	if v.Tag == "" {
		v.Tag = imageVersionLabel(data)
	}
	if v.Tag == "" && v.Digest != "" {
		if tag, err := c.LookupVersionTag(ctx, ref, v.Digest); err == nil {
			v.Tag = tag
		}
	}
	return v, nil
}

func imageVersionLabel(data map[string]any) string {
	labels := imageLabels(data)
	for _, key := range []string{"org.opencontainers.image.version", "org.label-schema.version", "version"} {
		if v := strings.TrimSpace(str(labels[key])); v != "" && v != "latest" && v != "main" && v != "nightly" {
			return v
		}
	}
	return ""
}

func imageRevisionLabel(data map[string]any) string {
	labels := imageLabels(data)
	for _, key := range []string{"org.opencontainers.image.revision", "org.label-schema.vcs-ref", "vcs-ref"} {
		if v := strings.TrimSpace(str(labels[key])); v != "" {
			return v
		}
	}
	return ""
}

func imageLabels(data map[string]any) map[string]any {
	cfg, _ := data["Config"].(map[string]any)
	labels, _ := cfg["Labels"].(map[string]any)
	return labels
}

func (c *Client) InspectImageVersionByID(ctx context.Context, imageID string) (ImageVersion, error) {
	return c.InspectImageVersionByIDWithRef(ctx, imageID, "")
}

func (c *Client) InspectLocalImageVersionByIDWithRef(ctx context.Context, imageID, imageRef string) (ImageVersion, error) {
	imageID = strings.TrimSpace(imageID)
	if imageID == "" {
		return ImageVersion{}, fmt.Errorf("empty image id")
	}
	var data map[string]any
	if err := c.doJSON(ctx, http.MethodGet, "/images/"+url.PathEscape(imageID)+"/json", nil, &data); err != nil {
		return ImageVersion{}, err
	}
	v := ImageVersion{Ref: strings.TrimSpace(imageRef), ID: imageID}
	if id, _ := data["Id"].(string); id != "" {
		v.ID = id
	} else if id, _ := data["ID"].(string); id != "" {
		v.ID = id
	}
	v.Revision = imageRevisionLabel(data)
	if tag := tagFromRef(imageRef); tag != "" && tag != "latest" && !isBareDigestTag(tag) {
		v.Tag = tag
	}
	if v.Tag == "" {
		v.Tag = imageVersionLabel(data)
	}
	if digests, _ := data["RepoDigests"].([]any); len(digests) > 0 {
		for _, raw := range digests {
			d := str(raw)
			if strings.Contains(d, "@sha256:") {
				v.Digest = d
				break
			}
		}
	}
	return v, nil
}

func (c *Client) InspectImageVersionByIDWithRef(ctx context.Context, imageID, imageRef string) (ImageVersion, error) {
	imageID = strings.TrimSpace(imageID)
	if imageID == "" {
		return ImageVersion{}, fmt.Errorf("empty image id")
	}
	v, err := c.InspectImageVersion(ctx, imageID)
	if err != nil {
		return v, err
	}
	if v.ID == "" {
		v.ID = imageID
	}
	imageRef = strings.TrimSpace(imageRef)
	if v.Ref == "" {
		v.Ref = imageRef
	}
	if imageRef != "" {
		if v.Ref == "" {
			v.Ref = imageRef
		}
		c.EnrichRemoteVersionTag(ctx, &v, imageRef)
	}
	if v.Tag == "" && v.RemoteTag == "" {
		c.EnrichBundledAppVersion(ctx, &v, imageRef)
	}
	return v, nil
}

func (c *Client) EnrichBundledAppVersion(ctx context.Context, v *ImageVersion, ref string) {
	if v == nil || strings.TrimSpace(v.Tag) != "" {
		return
	}
	lowRef := strings.ToLower(ref)
	image := strings.TrimSpace(v.ID)
	if image == "" {
		image = strings.TrimSpace(v.Ref)
	}
	if image == "" {
		return
	}
	switch {
	case strings.Contains(lowRef, "xream/sub-store"):
		if tag, err := c.readBundledVersion(ctx, image, "/opt/app/sub-store.bundle.js", 8<<20, subStoreBundleVersionRE); err == nil && tag != "" {
			v.Tag = tag
		}
	case strings.Contains(lowRef, "kwxos/tgbot-rss"):
		if tag, err := c.readBundledVersion(ctx, image, "/app/TGBot_RSS", 32<<20, tgbotRSSVersionRE); err == nil && tag != "" {
			v.Tag = tag
		}
	case strings.Contains(lowRef, "ghcr.io/shui1iao/guko"):
		if tag, err := c.readBundledVersion(ctx, image, "/app/bot.py", 1<<20, gukoVersionRE); err == nil && tag != "" {
			v.Tag = tag
		}
	default:
		for _, path := range []string{"/app/package.json", "/usr/src/app/package.json"} {
			if tag, err := c.readBundledVersion(ctx, image, path, 1<<20, packageJSONVersionRE); err == nil && tag != "" {
				v.Tag = tag
				return
			}
		}
	}
}

func (c *Client) readBundledVersion(ctx context.Context, image, path string, maxBytes int64, re *regexp.Regexp) (string, error) {
	body := map[string]any{
		"Image": image,
		"Cmd":   []string{"true"},
	}
	var created createResp
	if err := c.doJSON(ctx, http.MethodPost, "/containers/create", body, &created); err != nil {
		return "", err
	}
	defer func() {
		_ = c.delete(context.Background(), "/containers/"+url.PathEscape(created.ID)+"?force=true&v=false")
	}()

	resp, err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(created.ID)+"/archive?path="+url.QueryEscape(path), nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	tr := tar.NewReader(io.LimitReader(resp.Body, maxBytes+1<<20))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if h.FileInfo().IsDir() {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxBytes))
		if err != nil {
			return "", err
		}
		if m := re.FindSubmatch(b); len(m) > 1 {
			return string(m[1]), nil
		}
	}
	return "", fmt.Errorf("bundled version not found in %s", path)
}

func (c *Client) EnrichRemoteVersionTag(ctx context.Context, v *ImageVersion, ref string) {
	if v == nil || strings.TrimSpace(v.RemoteTag) != "" {
		return
	}
	if strings.TrimSpace(ref) == "" {
		ref = v.Ref
	}
	if strings.TrimSpace(ref) == "" || strings.TrimSpace(v.Digest) == "" {
		return
	}
	if tag, err := c.LookupVersionTag(ctx, ref, v.Digest); err == nil {
		v.RemoteTag = tag
		return
	}
	if tag, err := c.LookupVersionTagByRevision(ctx, ref, v.Revision); err == nil {
		v.RemoteTag = tag
	}
}

func (c *Client) RemoteImageVersion(ctx context.Context, ref string) (ImageVersion, error) {
	if err := c.PullImage(ctx, ref); err != nil {
		return ImageVersion{}, err
	}
	v, err := c.InspectImageVersion(ctx, ref)
	if err != nil {
		return v, err
	}
	c.EnrichRemoteVersionTag(ctx, &v, ref)
	if v.Tag == "" {
		v.Tag = v.RemoteTag
	}
	return v, nil
}

func (c *Client) LookupVersionTag(ctx context.Context, ref, digest string) (string, error) {
	digest = canonicalDigest(digest)
	if digest == "" {
		return "", fmt.Errorf("empty digest")
	}
	if registry, repo, ok := registryRepo(ref); ok && registry == "ghcr.io" {
		return lookupOCIRegistryVersionTag(ctx, registry, repo, digest)
	}
	return lookupDockerHubVersionTag(ctx, ref, digest)
}

func (c *Client) LookupVersionTagByRevision(ctx context.Context, ref, revision string) (string, error) {
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return "", fmt.Errorf("empty revision")
	}
	if registry, repo, ok := registryRepo(ref); ok && registry == "ghcr.io" {
		return lookupOCIRegistryVersionTagByRevision(ctx, registry, repo, revision)
	}
	return "", fmt.Errorf("revision tag lookup is not supported for %s", ref)
}

func lookupDockerHubVersionTag(ctx context.Context, ref, digest string) (string, error) {
	repo, err := dockerHubRepo(ref)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=100", repo)
	candidates := []string{}

	for page := 0; page < 8 && url != ""; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("Docker Hub tags request failed: %s", resp.Status)
		}

		var parsed struct {
			Next    string `json:"next"`
			Results []struct {
				Name   string        `json:"name"`
				Digest string        `json:"digest"`
				Images []digestImage `json:"images"`
			} `json:"results"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return "", err
		}
		for _, tag := range parsed.Results {
			if !tagMatchesDigest(tag.Digest, tag.Images, digest) {
				continue
			}
			if tag.Name == "latest" || strings.HasSuffix(tag.Name, "-latest") {
				continue
			}
			candidates = append(candidates, tag.Name)
		}
		url = parsed.Next
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no matching tag found for %s", digest)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return tagScore(candidates[i]) > tagScore(candidates[j])
	})
	return candidates[0], nil
}

func lookupOCIRegistryVersionTag(ctx context.Context, registry, repo, digest string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	token, err := ociRegistryToken(ctx, client, registry, repo)
	if err != nil {
		return "", err
	}
	tags, err := ociRegistryTags(ctx, client, registry, repo, token)
	if err != nil {
		return "", err
	}
	candidates := []string{}
	for _, tag := range tags {
		if tag == "" || tag == "latest" || strings.HasSuffix(tag, "-latest") || isBareDigestTag(tag) {
			continue
		}
		if !semverTagRE.MatchString(tag) {
			continue
		}
		manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repo, url.PathEscape(tag))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", ociManifestAcceptHeader())
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		tagDigest := resp.Header.Get("Docker-Content-Digest")
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && canonicalDigest(tagDigest) == digest {
			candidates = append(candidates, tag)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no matching tag found for %s", digest)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return tagScore(candidates[i]) > tagScore(candidates[j])
	})
	return candidates[0], nil
}

func lookupOCIRegistryVersionTagByRevision(ctx context.Context, registry, repo, revision string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	token, err := ociRegistryToken(ctx, client, registry, repo)
	if err != nil {
		return "", err
	}
	tags, err := ociRegistryTags(ctx, client, registry, repo, token)
	if err != nil {
		return "", err
	}
	candidates := []string{}
	for _, tag := range tags {
		if tag == "" || tag == "latest" || strings.HasSuffix(tag, "-latest") || isBareDigestTag(tag) || !semverTagRE.MatchString(tag) {
			continue
		}
		tagRevision, err := ociManifestRevision(ctx, client, registry, repo, token, tag)
		if err == nil && strings.EqualFold(strings.TrimSpace(tagRevision), revision) {
			candidates = append(candidates, tag)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no matching revision tag found for %s", revision)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return tagScore(candidates[i]) > tagScore(candidates[j])
	})
	return candidates[0], nil
}

func ociRegistryToken(ctx context.Context, client *http.Client, registry, repo string) (string, error) {
	tokenURL := fmt.Sprintf("https://%s/token?service=%s&scope=%s", registry, registry, url.QueryEscape("repository:"+repo+":pull"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("registry token request failed: %s", resp.Status)
	}
	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}
	if tokenResp.Token == "" {
		return "", fmt.Errorf("empty registry token")
	}
	return tokenResp.Token, nil
}

func ociRegistryTags(ctx context.Context, client *http.Client, registry, repo, token string) ([]string, error) {
	tagsURL := fmt.Sprintf("https://%s/v2/%s/tags/list?n=1000", registry, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("registry tags request failed: %s", resp.Status)
	}
	var tagsResp struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return nil, err
	}
	return tagsResp.Tags, nil
}

func ociManifestRevision(ctx context.Context, client *http.Client, registry, repo, token, ref string) (string, error) {
	manifest, err := ociManifest(ctx, client, registry, repo, token, ref)
	if err != nil {
		return "", err
	}
	if cfg, _ := manifest["config"].(map[string]any); cfg != nil {
		if digest := strings.TrimSpace(str(cfg["digest"])); digest != "" {
			return ociConfigRevision(ctx, client, registry, repo, token, digest)
		}
	}
	manifests, _ := manifest["manifests"].([]any)
	if len(manifests) == 0 {
		return "", fmt.Errorf("manifest has no config")
	}
	childDigest := ""
	for _, raw := range manifests {
		m, _ := raw.(map[string]any)
		platform, _ := m["platform"].(map[string]any)
		if str(platform["os"]) == "linux" && str(platform["architecture"]) == "amd64" {
			childDigest = strings.TrimSpace(str(m["digest"]))
			break
		}
	}
	if childDigest == "" {
		m, _ := manifests[0].(map[string]any)
		childDigest = strings.TrimSpace(str(m["digest"]))
	}
	if childDigest == "" {
		return "", fmt.Errorf("manifest index has no child digest")
	}
	return ociManifestRevision(ctx, client, registry, repo, token, childDigest)
}

func ociManifest(ctx context.Context, client *http.Client, registry, repo, token, ref string) (map[string]any, error) {
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repo, url.PathEscape(ref))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", ociManifestAcceptHeader())
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("registry manifest request failed: %s", resp.Status)
	}
	var manifest map[string]any
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func ociConfigRevision(ctx context.Context, client *http.Client, registry, repo, token, digest string) (string, error) {
	blobURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repo, url.PathEscape(digest))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, blobURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("registry config request failed: %s", resp.Status)
	}
	var cfg map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		return "", err
	}
	config, _ := cfg["config"].(map[string]any)
	labels, _ := config["Labels"].(map[string]any)
	for _, key := range []string{"org.opencontainers.image.revision", "org.label-schema.vcs-ref", "vcs-ref"} {
		if v := strings.TrimSpace(str(labels[key])); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("revision label not found")
}

func ociManifestAcceptHeader() string {
	return strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ", ")
}

type digestImage struct{ Digest string }

func tagMatchesDigest(tagDigest string, images []digestImage, digest string) bool {
	if canonicalDigest(tagDigest) == digest {
		return true
	}
	for _, img := range images {
		if canonicalDigest(img.Digest) == digest {
			return true
		}
	}
	return false
}

func tagScore(tag string) int {
	score := 0
	if semverTagRE.MatchString(tag) {
		score += 100
	}
	if strings.Contains(tag, "-") {
		score -= 10
	}
	if strings.Contains(tag, "latest") {
		score -= 50
	}
	return score
}

func registryRepo(ref string) (string, string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.Contains(ref, "://") || strings.Contains(ref, "@") {
		return "", "", false
	}
	parts := strings.Split(ref, "/")
	if len(parts) < 2 || !strings.Contains(parts[0], ".") {
		return "", "", false
	}
	registry := parts[0]
	last := parts[len(parts)-1]
	if i := strings.LastIndex(last, ":"); i >= 0 {
		last = last[:i]
		parts[len(parts)-1] = last
	}
	return registry, strings.Join(parts[1:], "/"), true
}

func dockerHubRepo(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.Contains(ref, "://") || strings.Contains(ref, "@") {
		return "", fmt.Errorf("unsupported image ref %q", ref)
	}

	name := ref
	parts := strings.Split(name, "/")
	if len(parts) > 0 && strings.Contains(parts[0], ".") {
		if parts[0] != "docker.io" && parts[0] != "index.docker.io" && parts[0] != "registry-1.docker.io" {
			return "", fmt.Errorf("non Docker Hub registry is not supported for tag lookup: %s", parts[0])
		}
		parts = parts[1:]
	}
	if len(parts) == 1 {
		parts = []string{"library", parts[0]}
	}
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid image ref %q", ref)
	}
	last := parts[len(parts)-1]
	if i := strings.LastIndex(last, ":"); i >= 0 {
		last = last[:i]
		parts[len(parts)-1] = last
	}
	return strings.Join(parts, "/"), nil
}

func tagFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.Contains(ref, "@") {
		return ""
	}
	lastSlash := strings.LastIndex(ref, "/")
	lastColon := strings.LastIndex(ref, ":")
	if lastColon <= lastSlash {
		return ""
	}
	return ref[lastColon+1:]
}

func digestFromRef(ref string) string {
	if i := strings.LastIndex(ref, "@sha256:"); i >= 0 {
		return ref[i+1:]
	}
	return ""
}

func canonicalDigest(digest string) string {
	digest = strings.TrimSpace(digest)
	if i := strings.LastIndex(digest, "@"); i >= 0 {
		digest = digest[i+1:]
	}
	if strings.HasPrefix(digest, "sha256:") {
		return digest
	}
	return ""
}

func normalizeImageID(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "sha256:")
}

func releaseTag(v ImageVersion) string {
	for _, tag := range []string{v.Tag, v.RemoteTag} {
		tag = strings.TrimSpace(tag)
		if semverTagRE.MatchString(tag) {
			return tag
		}
	}
	return ""
}

func isFloatingRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "sha256:") || strings.Contains(ref, "@sha256:") || isBareDigestTag(ref) {
		return false
	}
	tag := tagFromRef(ref)
	if tag == "" {
		return true
	}
	return tag == "latest" || tag == "main" || tag == "nightly" || strings.HasSuffix(tag, "-latest")
}

func shortID(id string) string {
	id = normalizeImageID(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

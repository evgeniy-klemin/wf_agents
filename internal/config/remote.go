package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	remoteCacheTTL = time.Hour
)

// RemoteRef holds the parsed components of a remote preset reference.
type RemoteRef struct {
	Provider string // "gitlab" or "github"
	Project  string // e.g. "iriski/wf-presets"
	Path     string // e.g. "go-service" or "presets/iriski"
	Ref      string // optional tag/branch/commit; "" means default branch
}

// ParseRemoteRef parses a remote preset URI into a RemoteRef.
// Format: <provider>:<project>//<path>[@ref]
// Supported providers: gitlab, github.
func ParseRemoteRef(s string) (RemoteRef, error) {
	var provider string
	switch {
	case strings.HasPrefix(s, "gitlab:"):
		provider = "gitlab"
		s = strings.TrimPrefix(s, "gitlab:")
	case strings.HasPrefix(s, "github:"):
		provider = "github"
		s = strings.TrimPrefix(s, "github:")
	default:
		return RemoteRef{}, fmt.Errorf("unsupported provider in %q: must be gitlab: or github", s)
	}

	// Split on "//" to separate project from path
	parts := strings.SplitN(s, "//", 2)
	if len(parts) != 2 {
		return RemoteRef{}, fmt.Errorf("invalid remote ref %q: expected format <project>//<path>[@ref]", s)
	}
	project := parts[0]
	rest := parts[1]

	// Split optional @ref suffix from path
	var path, ref string
	if idx := strings.LastIndex(rest, "@"); idx != -1 {
		path = rest[:idx]
		ref = rest[idx+1:]
	} else {
		path = rest
	}

	return RemoteRef{Provider: provider, Project: project, Path: path, Ref: ref}, nil
}

// cacheBaseDir returns the base cache directory for remote presets.
func cacheBaseDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "wf-agents", "presets")
}

// execRunner is the default command runner using os/exec.
func execRunner(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// FetchRemotePreset fetches a remote preset and returns the local cache directory.
func FetchRemotePreset(ref RemoteRef) (string, error) {
	sha, err := resolveRefSHA(ref, execRunner)
	if err != nil {
		// Try stale cache fallback before giving up
		dir, fallbackErr := staleCacheFallback(cacheBaseDir(), ref)
		if fallbackErr == nil {
			fmt.Fprintf(os.Stderr, "Warning: wf-agents: failed to resolve remote preset SHA (%v); using stale cache\n", err)
			return dir, nil
		}
		return "", fmt.Errorf("resolve remote preset %q: %w", ref.Provider+":"+ref.Project+"//"+ref.Path, err)
	}

	return fetchRemotePresetWithRunner(ref, cacheBaseDir(), sha, execRunner)
}

// fetchRemotePresetWithRunner is the testable core of FetchRemotePreset.
func fetchRemotePresetWithRunner(ref RemoteRef, cacheBase, sha string, runner func(string, ...string) ([]byte, error)) (string, error) {
	cacheDir := filepath.Join(cacheBase, sha)

	// Check if we have a fresh cache
	if isCacheFresh(cacheDir) {
		return cacheDir, nil
	}

	// For a full 40-char commit SHA, cache is immutable — download once if dir exists
	isFullSHA := len(sha) == 40 && isHex(sha)
	if isFullSHA {
		if _, err := os.Stat(cacheDir); err == nil {
			writeCheckedAt(cacheDir, sha)
			return cacheDir, nil
		}
	}

	// Download files into cache
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	if err := downloadPresetFiles(ref, sha, cacheDir, runner); err != nil {
		// If download fails but stale cache dir exists, use it
		if _, statErr := os.Stat(filepath.Join(cacheDir, ".checked_at")); statErr == nil {
			fmt.Fprintf(os.Stderr, "Warning: wf-agents: failed to refresh preset cache (%v); using stale cache\n", err)
			return cacheDir, nil
		}
		return "", err
	}

	writeCheckedAt(cacheDir, sha)
	return cacheDir, nil
}

// checkedAtFile holds the cache metadata.
type checkedAtFile struct {
	CheckedAt int64  `json:"checked_at"`
	SHA       string `json:"sha"`
}

func isCacheFresh(cacheDir string) bool {
	data, err := os.ReadFile(filepath.Join(cacheDir, ".checked_at"))
	if err != nil {
		return false
	}
	var meta checkedAtFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return false
	}
	age := time.Since(time.Unix(meta.CheckedAt, 0))
	return age < remoteCacheTTL
}

func writeCheckedAt(cacheDir, sha string) {
	meta := checkedAtFile{CheckedAt: time.Now().Unix(), SHA: sha}
	data, _ := json.Marshal(meta)
	_ = os.WriteFile(filepath.Join(cacheDir, ".checked_at"), data, 0644)
}

// staleCacheFallback returns any existing cache entry directory for this cacheBase.
func staleCacheFallback(cacheBase string, ref RemoteRef) (string, error) {
	entries, err := os.ReadDir(cacheBase)
	if err != nil {
		return "", fmt.Errorf("no cache available")
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(cacheBase, e.Name())
		if _, err := os.Stat(filepath.Join(dir, ".checked_at")); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no stale cache found for %s:%s//%s", ref.Provider, ref.Project, ref.Path)
}

// resolveRefSHA resolves a RemoteRef to a commit SHA using the appropriate CLI.
func resolveRefSHA(ref RemoteRef, runner func(string, ...string) ([]byte, error)) (string, error) {
	if len(ref.Ref) == 40 && isHex(ref.Ref) {
		return ref.Ref, nil
	}
	switch ref.Provider {
	case "gitlab":
		return resolveGitLabSHA(ref, runner)
	case "github":
		return resolveGitHubSHA(ref, runner)
	default:
		return "", fmt.Errorf("unsupported provider: %s", ref.Provider)
	}
}

func resolveGitLabSHA(ref RemoteRef, runner func(string, ...string) ([]byte, error)) (string, error) {
	projectEncoded := url.PathEscape(ref.Project)
	refParam := ref.Ref
	if refParam == "" {
		refParam = "HEAD"
	}
	apiPath := fmt.Sprintf("projects/%s/repository/commits/%s", projectEncoded, url.PathEscape(refParam))
	out, err := runner("glab", "api", apiPath)
	if err != nil {
		return "", fmt.Errorf("glab api %s: %w", apiPath, err)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &result); err != nil || result.ID == "" {
		return "", fmt.Errorf("parse commit SHA from glab output: %w", err)
	}
	return result.ID, nil
}

func resolveGitHubSHA(ref RemoteRef, runner func(string, ...string) ([]byte, error)) (string, error) {
	refParam := ref.Ref
	if refParam == "" {
		refParam = "HEAD"
	}
	apiPath := fmt.Sprintf("repos/%s/commits/%s", ref.Project, refParam)
	out, err := runner("gh", "api", apiPath)
	if err != nil {
		return "", fmt.Errorf("gh api %s: %w", apiPath, err)
	}
	var result struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(out, &result); err != nil || result.SHA == "" {
		return "", fmt.Errorf("parse commit SHA from gh output: %w", err)
	}
	return result.SHA, nil
}

// downloadPresetFiles downloads workflow.yaml and *.md files from the remote preset path.
func downloadPresetFiles(ref RemoteRef, sha, destDir string, runner func(string, ...string) ([]byte, error)) error {
	switch ref.Provider {
	case "gitlab":
		return downloadGitLabFiles(ref, sha, destDir, runner)
	case "github":
		return downloadGitHubFiles(ref, sha, destDir, runner)
	default:
		return fmt.Errorf("unsupported provider: %s", ref.Provider)
	}
}

func downloadGitLabFiles(ref RemoteRef, sha, destDir string, runner func(string, ...string) ([]byte, error)) error {
	projectEncoded := url.PathEscape(ref.Project)
	apiPath := fmt.Sprintf("projects/%s/repository/tree?path=%s&ref=%s",
		projectEncoded, url.QueryEscape(ref.Path), url.QueryEscape(sha))

	out, err := runner("glab", "api", apiPath)
	if err != nil {
		return fmt.Errorf("list files via glab api: %w", err)
	}

	var items []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(out, &items); err != nil {
		return fmt.Errorf("parse tree response: %w", err)
	}

	for _, item := range items {
		if item.Type != "blob" {
			continue
		}
		if item.Name != "workflow.yaml" && !strings.HasSuffix(item.Name, ".md") {
			continue
		}
		filePath := ref.Path + "/" + item.Name
		filePathEncoded := url.PathEscape(filePath)
		fileAPI := fmt.Sprintf("projects/%s/repository/files/%s/raw?ref=%s",
			projectEncoded, filePathEncoded, url.QueryEscape(sha))
		content, err := runner("glab", "api", fileAPI)
		if err != nil {
			return fmt.Errorf("download file %s: %w", item.Name, err)
		}
		if err := os.WriteFile(filepath.Join(destDir, item.Name), content, 0644); err != nil {
			return fmt.Errorf("write file %s: %w", item.Name, err)
		}
	}
	return nil
}

func downloadGitHubFiles(ref RemoteRef, sha, destDir string, runner func(string, ...string) ([]byte, error)) error {
	apiPath := fmt.Sprintf("repos/%s/contents/%s?ref=%s", ref.Project, ref.Path, sha)
	out, err := runner("gh", "api", apiPath)
	if err != nil {
		return fmt.Errorf("list files via gh api: %w", err)
	}

	var items []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(out, &items); err != nil {
		return fmt.Errorf("parse contents response: %w", err)
	}

	for _, item := range items {
		if item.Type != "file" {
			continue
		}
		if item.Name != "workflow.yaml" && !strings.HasSuffix(item.Name, ".md") {
			continue
		}
		fileAPI := fmt.Sprintf("repos/%s/contents/%s/%s?ref=%s", ref.Project, ref.Path, item.Name, sha)
		out, err := runner("gh", "api", fileAPI)
		if err != nil {
			return fmt.Errorf("download file %s: %w", item.Name, err)
		}
		// gh api returns base64-encoded content in a JSON envelope
		var fileResp struct {
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		}
		var content []byte
		if jsonErr := json.Unmarshal(out, &fileResp); jsonErr == nil && fileResp.Encoding == "base64" {
			raw := strings.ReplaceAll(fileResp.Content, "\n", "")
			decoded, err := base64.StdEncoding.DecodeString(raw)
			if err != nil {
				return fmt.Errorf("decode file %s: %w", item.Name, err)
			}
			content = decoded
		} else {
			content = out
		}
		if err := os.WriteFile(filepath.Join(destDir, item.Name), content, 0644); err != nil {
			return fmt.Errorf("write file %s: %w", item.Name, err)
		}
	}
	return nil
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

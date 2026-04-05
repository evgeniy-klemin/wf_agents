package platform

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// CmdRunner is a function type for running external commands, used to allow
// dependency injection in evidence collectors for testability.
type CmdRunner func(timeout time.Duration, name string, args ...string) (string, error)

// RunCmd runs an external command with the given timeout and returns combined output.
func RunCmd(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// RunCmdInDir runs an external command with the given timeout in the specified
// directory and returns combined output. Used for tools that don't support -C.
func RunCmdInDir(timeout time.Duration, dir string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ParsePlatformFromURL detects the git hosting platform from a remote URL.
func ParsePlatformFromURL(remoteURL string) string {
	if strings.Contains(remoteURL, "github.com") {
		return "github"
	}
	// Extract just the host portion for gitlab detection to avoid matching
	// paths that contain "gitlab" as part of an organization or repo name.
	// Works for both SSH (git@host:org/repo) and HTTPS (https://host/org/repo).
	host := remoteURL
	if idx := strings.Index(remoteURL, "://"); idx >= 0 {
		host = remoteURL[idx+3:]
	} else if idx := strings.Index(remoteURL, "@"); idx >= 0 {
		host = remoteURL[idx+1:]
	}
	if idx := strings.IndexAny(host, ":/"); idx >= 0 {
		host = host[:idx]
	}
	if strings.Contains(host, "gitlab") {
		return "gitlab"
	}
	return "unknown"
}

// GitRemoteToWebURL converts a git remote URL to a browsable HTTPS URL.
// Handles both SSH (git@host:org/repo.git) and HTTPS (https://host/org/repo.git) forms.
// Returns empty string if input is empty or unparseable.
func GitRemoteToWebURL(remoteURL string) string {
	if remoteURL == "" {
		return ""
	}
	u := strings.TrimSpace(remoteURL)
	u = strings.TrimSuffix(u, ".git")

	// SSH form: git@host:path
	if !strings.Contains(u, "://") {
		if idx := strings.Index(u, "@"); idx >= 0 {
			u = u[idx+1:]
		} else {
			return ""
		}
		colonIdx := strings.Index(u, ":")
		if colonIdx < 0 {
			return ""
		}
		host := u[:colonIdx]
		path := u[colonIdx+1:]
		return "https://" + host + "/" + path
	}

	// HTTPS form: already has scheme
	return u
}

// ProjectNameFromURL extracts the last path segment (repo name) from a git remote URL,
// stripping the .git suffix. Returns empty string if input is empty.
func ProjectNameFromURL(remoteURL string) string {
	if remoteURL == "" {
		return ""
	}
	u := strings.TrimSpace(remoteURL)
	u = strings.TrimSuffix(u, ".git")
	// Get last segment after "/" or ":"
	if idx := strings.LastIndexAny(u, "/:"); idx >= 0 {
		return u[idx+1:]
	}
	return u
}

// DetectPlatform determines the git hosting platform of the current repo.
func DetectPlatform() string {
	out, err := RunCmd(5*time.Second, "git", "remote", "get-url", "origin")
	if err != nil {
		return "unknown"
	}
	return ParsePlatformFromURL(strings.TrimSpace(out))
}

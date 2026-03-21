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

// DetectPlatform determines the git hosting platform of the current repo.
func DetectPlatform() string {
	out, err := RunCmd(5*time.Second, "git", "remote", "get-url", "origin")
	if err != nil {
		return "unknown"
	}
	return ParsePlatformFromURL(strings.TrimSpace(out))
}

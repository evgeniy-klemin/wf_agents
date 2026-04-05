package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRemoteRef_GitLab(t *testing.T) {
	ref, err := ParseRemoteRef("gitlab:iriski/wf-presets//go-service")
	require.NoError(t, err)
	assert.Equal(t, "gitlab", ref.Provider)
	assert.Equal(t, "iriski/wf-presets", ref.Project)
	assert.Equal(t, "go-service", ref.Path)
	assert.Equal(t, "", ref.Ref)
}

func TestParseRemoteRef_GitLabWithRef(t *testing.T) {
	ref, err := ParseRemoteRef("gitlab:iriski/wf-presets//go-service@v1.0")
	require.NoError(t, err)
	assert.Equal(t, "gitlab", ref.Provider)
	assert.Equal(t, "iriski/wf-presets", ref.Project)
	assert.Equal(t, "go-service", ref.Path)
	assert.Equal(t, "v1.0", ref.Ref)
}

func TestParseRemoteRef_GitLabCommitSHA(t *testing.T) {
	ref, err := ParseRemoteRef("gitlab:evgeniy.klemin/wf_agents//presets/iriski@abc123f")
	require.NoError(t, err)
	assert.Equal(t, "gitlab", ref.Provider)
	assert.Equal(t, "evgeniy.klemin/wf_agents", ref.Project)
	assert.Equal(t, "presets/iriski", ref.Path)
	assert.Equal(t, "abc123f", ref.Ref)
}

func TestParseRemoteRef_GitHub(t *testing.T) {
	ref, err := ParseRemoteRef("github:org/repo//presets/python")
	require.NoError(t, err)
	assert.Equal(t, "github", ref.Provider)
	assert.Equal(t, "org/repo", ref.Project)
	assert.Equal(t, "presets/python", ref.Path)
	assert.Equal(t, "", ref.Ref)
}

func TestParseRemoteRef_InvalidNoDoubleSep(t *testing.T) {
	_, err := ParseRemoteRef("gitlab:iriski/wf-presets/go-service")
	assert.Error(t, err)
}

func TestParseRemoteRef_InvalidProvider(t *testing.T) {
	_, err := ParseRemoteRef("bitbucket:org/repo//path")
	assert.Error(t, err)
}

func TestFetchRemotePreset_CacheHit(t *testing.T) {
	// Create a warm cache directory (checked_at within TTL)
	cacheBase := t.TempDir()
	sha := "abcdef1234567890abcdef1234567890abcdef12"
	cacheDir := filepath.Join(cacheBase, sha)
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Write checked_at file with current time (fresh cache)
	checkedAt := map[string]interface{}{
		"checked_at": time.Now().Unix(),
		"sha":        sha,
	}
	data, _ := json.Marshal(checkedAt)
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, ".checked_at"), data, 0644))

	// Write a fake workflow.yaml in the cache
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "workflow.yaml"), []byte("tracking: {}"), 0644))

	// A runner that fails if called — cache should be fresh so exec is skipped
	calledExec := false
	runner := func(name string, args ...string) ([]byte, error) {
		calledExec = true
		return nil, nil
	}

	ref := RemoteRef{Provider: "gitlab", Project: "test/project", Path: "preset/path", Ref: ""}
	dir, err := fetchRemotePresetWithRunner(ref, cacheBase, sha, runner)
	require.NoError(t, err)
	assert.Equal(t, cacheDir, dir)
	assert.False(t, calledExec, "exec should not be called when cache is fresh")
}

func TestFetchRemotePreset_StaleCacheFallback(t *testing.T) {
	// Create a stale cache (checked_at expired)
	cacheBase := t.TempDir()
	sha := "abcdef1234567890abcdef1234567890abcdef12"
	cacheDir := filepath.Join(cacheBase, sha)
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Write checked_at file with old time (stale)
	checkedAt := map[string]interface{}{
		"checked_at": time.Now().Add(-2 * time.Hour).Unix(),
		"sha":        sha,
	}
	data, _ := json.Marshal(checkedAt)
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, ".checked_at"), data, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "workflow.yaml"), []byte("tracking: {}"), 0644))

	// Runner fails (CLI not available)
	runner := func(name string, args ...string) ([]byte, error) {
		return nil, assert.AnError
	}

	ref := RemoteRef{Provider: "gitlab", Project: "test/project", Path: "preset/path", Ref: ""}
	// Should fall back to stale cache with warning, not error
	dir, err := fetchRemotePresetWithRunner(ref, cacheBase, sha, runner)
	require.NoError(t, err)
	assert.Equal(t, cacheDir, dir)
}

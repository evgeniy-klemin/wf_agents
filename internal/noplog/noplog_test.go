package noplog_test

import (
	"testing"

	"github.com/eklemin/wf-agents/internal/noplog"
	"go.temporal.io/sdk/log"
)

// TestNew verifies that New() returns a value that satisfies the log.Logger interface.
// The no-op logger exists solely to suppress the "No logger configured" warning from
// the Temporal client; its methods must be callable without panicking.
func TestNew(t *testing.T) {
	var _ log.Logger = noplog.New() //nolint:staticcheck
}

func TestNoplogMethodsDoNotPanic(t *testing.T) {
	l := noplog.New()

	// All four log levels must be callable with any number of key-value pairs.
	l.Debug("debug message")
	l.Debug("debug with kv", "key", "value")
	l.Info("info message")
	l.Info("info with kv", "key", "value")
	l.Warn("warn message")
	l.Warn("warn with kv", "key", "value")
	l.Error("error message")
	l.Error("error with kv", "key", "value")
}

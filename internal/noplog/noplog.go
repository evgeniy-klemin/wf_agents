// Package noplog provides a no-op implementation of go.temporal.io/sdk/log.Logger.
// It is used to suppress the "No logger configured for temporal client" warning that
// the Temporal SDK prints to stderr on every binary invocation when no logger is set.
package noplog

import "go.temporal.io/sdk/log"

// nopLogger silently discards all log messages.
type nopLogger struct{}

// New returns a log.Logger that discards all messages.
func New() log.Logger {
	return nopLogger{}
}

func (nopLogger) Debug(_ string, _ ...interface{}) {}
func (nopLogger) Info(_ string, _ ...interface{})  {}
func (nopLogger) Warn(_ string, _ ...interface{})  {}
func (nopLogger) Error(_ string, _ ...interface{}) {}

package temporal

import "os"

// Host returns the Temporal gRPC host address.
// Checks TEMPORAL_HOST env var, defaults to "localhost:7233".
func Host() string {
	if h := os.Getenv("TEMPORAL_HOST"); h != "" {
		return h
	}
	return "localhost:7233"
}

package fixtures

import "time"

const (
	// Timeout is the default timeout for waiting for a condition to be met.
	Timeout = 5 * time.Second
	// Interval is the default interval for polling for a condition to be met.
	Interval = 250 * time.Millisecond
	BaseDir  = "templates"
)

package env

import (
	"os"
	"strings"
)

// GetOrDefault returns the value of the environment variable if set and non-empty
// (after trimming whitespace), otherwise returns the provided default value.
func GetOrDefault(envVar, defaultValue string) string {
	v := strings.TrimSpace(os.Getenv(envVar))
	if v != "" {
		return v
	}
	return defaultValue
}

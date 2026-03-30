package flags

import "os"

// EnvOrDefault returns the value of the named environment variable or fallback if unset/empty.
func EnvOrDefault(envVar, defaultValue string) string {
	if value := os.Getenv(envVar); value != "" {
		return value
	}
	return defaultValue
}

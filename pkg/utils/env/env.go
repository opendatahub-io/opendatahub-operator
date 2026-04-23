package env

import "os"

// GetOrDefault returns the value of the named environment variable or fallback if unset/empty.
func GetOrDefault(envVar, defaultValue string) string {
	if value := os.Getenv(envVar); value != "" {
		return value
	}
	return defaultValue
}

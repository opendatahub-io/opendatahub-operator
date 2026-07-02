package timeout_test

import (
	"testing"
	"time"
)

func TestFastPass(t *testing.T) {}

func TestFastFail(t *testing.T) {
	t.Fatal("always fails")
}

func TestSlow(t *testing.T) {
	time.Sleep(10 * time.Second) // causes go test to panic with exit code 2 under short -timeout
}

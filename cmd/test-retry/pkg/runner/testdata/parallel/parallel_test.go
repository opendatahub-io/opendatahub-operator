package parallel

import (
	"testing"
	"time"
)

func TestParallel(t *testing.T) {
	t.Run("Test1", func(t *testing.T) {
		t.Parallel()
		time.Sleep(1 * time.Second)
		t.Log("Test1")
	})

	t.Run("Test2", func(t *testing.T) {
		t.Parallel()
		t.Fail()
	})

	t.Run("Test3", func(t *testing.T) {
		t.Parallel()
		t.Log("Test3")
	})
}

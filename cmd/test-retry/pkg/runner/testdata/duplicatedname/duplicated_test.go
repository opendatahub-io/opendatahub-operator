package duplicatedname

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDuplicated(t *testing.T) {
	t.Run("two of me", func(t *testing.T) {
		t.Log("This test passes")
	})

	t.Run("two of me", func(t *testing.T) {
		require.Fail(t, "This test always fails on retry")
	})
}

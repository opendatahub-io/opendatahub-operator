package siblings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSiblings(t *testing.T) {
	t.Run("nested", func(t *testing.T) {
		t.Run("sibling 1", func(t *testing.T) {
			require.Fail(t, "This test always fails")
		})
		t.Run("sibling 2", func(t *testing.T) {
			t.Log("This test passes")
		})
	})

	t.Run("pass 1", func(t *testing.T) {
		t.Log("This test passes")
	})
}

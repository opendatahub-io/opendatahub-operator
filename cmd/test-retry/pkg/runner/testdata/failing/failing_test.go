package failing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAlwaysFail1 always fails
func TestAlwaysFail1(t *testing.T) {
	require.Fail(t, "This test always fails")
}

// TestAlwaysFail2 always fails
func TestAlwaysFail2(t *testing.T) {
	require.Fail(t, "This test also always fails")
}

// TestPass always passes (to test mixed results)
func TestPass(t *testing.T) {
	t.Log("This test passes")
}

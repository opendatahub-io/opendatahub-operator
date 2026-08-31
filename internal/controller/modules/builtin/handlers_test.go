package builtin_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	modulebuiltin "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/builtin"
)

func TestHandlersAndRunlevelsMatch(t *testing.T) {
	t.Parallel()

	handlers := modulebuiltin.Handlers()
	runlevels := modulebuiltin.Runlevels()

	assert.Len(t, handlers, len(runlevels))

	for name := range handlers {
		_, ok := runlevels[name]
		assert.True(t, ok, "handler %q has no runlevel entry", name)
	}

	for name := range runlevels {
		_, ok := handlers[name]
		assert.True(t, ok, "runlevel %q has no handler entry", name)
	}
}

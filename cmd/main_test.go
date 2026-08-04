package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

func TestAllComponentsHaveExplicitRunlevel(t *testing.T) {
	t.Parallel()

	for name := range existingComponents {
		_, ok := componentRunlevels[name]
		assert.True(t, ok, "component %q is registered but has no entry in componentRunlevels — add an explicit runlevel assignment", name)
	}
}

func TestAllModulesHaveExplicitRunlevel(t *testing.T) {
	t.Parallel()

	for name := range existingModules {
		_, ok := moduleRunlevels[name]
		assert.True(t, ok, "module %q is registered but has no entry in moduleRunlevels — add an explicit runlevel assignment", name)
	}
}

func TestComponentRunlevelsOnlyReferenceRegisteredComponents(t *testing.T) {
	t.Parallel()

	for name := range componentRunlevels {
		_, ok := existingComponents[name]
		assert.True(t, ok, "componentRunlevels has entry %q but no matching handler in existingComponents — stale entry?", name)
	}
}

func TestModuleRunlevelsOnlyReferenceRegisteredModules(t *testing.T) {
	t.Parallel()

	for name := range moduleRunlevels {
		_, ok := existingModules[name]
		assert.True(t, ok, "moduleRunlevels has entry %q but no matching handler in existingModules — stale entry?", name)
	}
}

func TestNoComponentUsesRunlevelDefault(t *testing.T) {
	t.Parallel()

	for name, rl := range componentRunlevels {
		assert.NotEqual(t, dag.RL(99), rl,
			"component %q uses Runlevel99 — assign an explicit runlevel", name)
	}
}

func TestNoModuleUsesRunlevelDefault(t *testing.T) {
	t.Parallel()

	for name, rl := range moduleRunlevels {
		assert.NotEqual(t, dag.RL(99), rl,
			"module %q uses Runlevel99 — assign an explicit runlevel", name)
	}
}

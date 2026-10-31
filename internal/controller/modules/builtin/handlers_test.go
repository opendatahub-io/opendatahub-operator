package builtin_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	modulebuiltin "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/builtin"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

func TestRegistrationsHaveCompleteMetadata(t *testing.T) {
	t.Parallel()

	registrations := modulebuiltin.Registrations()
	require.NotEmpty(t, registrations)

	seen := make(map[string]struct{}, len(registrations))
	seenCRDs := make(map[string]struct{}, len(registrations))
	for _, registration := range registrations {
		require.NotNil(t, registration.Handler)

		name := registration.Handler.GetName()
		require.NotEmpty(t, name)
		if name != "sparkoperator" && name != "databaseservice" {
			require.NotEmpty(t, registration.BundleCRDName, "module %q needs a bundle-owned CRD", name)
		} else {
			require.Empty(t, registration.BundleCRDName, "removed module %q must not be bundle-owned", name)
		}
		if registration.BundleCRDName != "" {
			_, duplicateCRD := seenCRDs[registration.BundleCRDName]
			require.False(t, duplicateCRD, "duplicate bundle CRD for module %q", name)
			seenCRDs[registration.BundleCRDName] = struct{}{}
		}
		_, duplicate := seen[name]
		assert.False(t, duplicate, "duplicate registration for module %q", name)
		seen[name] = struct{}{}

		assert.NotEqual(t, dag.Runlevel{}, registration.Runlevel,
			"module %q has empty runlevel", name)
		assert.NotEqual(t, dag.RL(99), registration.Runlevel,
			"module %q has default runlevel", name)
		assert.Contains(t, []modules.ConfigSource{
			modules.ConfigFromDSC,
			modules.ConfigFromDSCI,
		}, registration.ConfigSource, "module %q has invalid config source", name)

		if name == serviceApi.MonitoringServiceName {
			assert.Equal(t, modules.ConfigFromDSCI, registration.ConfigSource)
		} else {
			assert.Equal(t, modules.ConfigFromDSC, registration.ConfigSource)
		}
	}
}

func TestBundleModuleCRDsMarkedInternal(t *testing.T) {
	t.Parallel()

	for _, csvPath := range []string{
		"config/manifests/bases/opendatahub-operator.clusterserviceversion.yaml",
		"config/rhoai/manifests/bases/rhods-operator.clusterserviceversion.yaml",
	} {
		t.Run(csvPath, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", csvPath))
			require.NoError(t, err)
			var csv struct {
				Metadata struct {
					Annotations map[string]string `json:"annotations"`
				} `json:"metadata"`
			}
			require.NoError(t, yaml.Unmarshal(data, &csv))
			var internal []string
			annotation := csv.Metadata.Annotations["operators.operatorframework.io/internal-objects"]
			require.NoError(t, json.Unmarshal([]byte(annotation), &internal))
			expected := []string{
				"kueues.components.platform.opendatahub.io",
				"platforms.config.opendatahub.io",
			}
			for _, registration := range modulebuiltin.Registrations() {
				if registration.BundleCRDName != "" {
					expected = append(expected, registration.BundleCRDName)
				}
			}
			require.ElementsMatch(t, expected, internal)
		})
	}
}

func TestNamesDerivedFromRegistrations(t *testing.T) {
	t.Parallel()

	registrations := modulebuiltin.Registrations()
	names := modulebuiltin.Names()
	require.Len(t, names, len(registrations))

	for i, registration := range registrations {
		assert.Equal(t, registration.Handler.GetName(), names[i])
	}
}

func TestRegisterPreservesConfigSource(t *testing.T) {
	t.Parallel()

	reg := &modules.Registry{}
	modulebuiltin.Register(reg)

	var dsciNames []string
	err := reg.ForConfigSource(modules.ConfigFromDSCI, func(handler modules.ModuleHandler, _ bool) error {
		dsciNames = append(dsciNames, handler.GetName())
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{serviceApi.MonitoringServiceName}, dsciNames)
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const testCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: examples.components.platform.opendatahub.io
spec:
  group: components.platform.opendatahub.io
  names:
    kind: Example
    plural: examples
  scope: Cluster
  versions:
  - name: v1alpha1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
`

func TestGenerateBundleCRDs(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "opt", "manifests", "example", "crd", "bases")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "example.yaml"), []byte(testCRD), 0o644))

	out := filepath.Join(root, "config", "crd", "components")
	src := source{
		name:  "examples.components.platform.opendatahub.io",
		gvk:   schema.GroupVersionKind{Group: "components.platform.opendatahub.io", Version: "v1alpha1", Kind: "Example"},
		roots: []string{filepath.Join(root, "opt", "manifests", "example")},
	}
	require.NoError(t, generate(out, []source{src}))
	actual, err := os.ReadFile(filepath.Join(out, src.name+".yaml"))
	require.NoError(t, err)
	require.Equal(t, testCRD, string(actual))
	kustomization, err := os.ReadFile(filepath.Join(out, "kustomization.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(kustomization), "- "+src.name+".yaml")

	// A missing source must fail before replacing an existing valid overlay.
	src.gvk.Kind = "Other"
	err = generate(out, []source{src})
	require.ErrorContains(t, err, "missing from")
	_, err = os.Stat(filepath.Join(out, src.name+".yaml"))
	require.NoError(t, err)
	require.NoError(t, restoreEmptyOverlay(out))
	_, err = os.Stat(filepath.Join(out, src.name+".yaml"))
	require.True(t, os.IsNotExist(err))
	kustomization, err = os.ReadFile(filepath.Join(out, "kustomization.yaml"))
	require.NoError(t, err)
	require.Equal(t, emptyKustomization, string(kustomization))
}

func TestFindCRDRejectsUnservedVersion(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "example.yaml"),
		[]byte(strings.Replace(testCRD, "served: true", "served: false", 1)), 0o644))
	_, err := findCRD(source{
		name:  "examples.components.platform.opendatahub.io",
		gvk:   schema.GroupVersionKind{Group: "components.platform.opendatahub.io", Version: "v1alpha1", Kind: "Example"},
		roots: []string{root},
	})
	require.ErrorContains(t, err, "does not serve v1alpha1")
}

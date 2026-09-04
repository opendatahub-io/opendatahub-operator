package v2_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
)

const (
	rhoaiCSVRelPath = "config/rhoai/manifests/bases/rhods-operator.clusterserviceversion.yaml"

	initResourceAnnotation = "operatorframework.io/initialization-resource"
)

// repoRoot resolves the repository root regardless of the test binary's
// working directory, using this file's own location via runtime.Caller. Walks
// up looking for .git rather than hardcoding a directory depth.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed to resolve this test file's path")

	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "no .git found above %s", thisFile)
		dir = parent
	}
}

// TestInitResourceAnnotationIsValidDSC guards the hand-maintained
// operatorframework.io/initialization-resource annotation on the rhoai CSV
// against the actual DataScienceCluster API schema. Unlike the drift check
// against the sample (TestInitResourceAnnotationMatchesSample), this catches a
// hand-edit -- or an API field rename -- that leaves the annotation (and a
// matching sample) referencing a field the v2 type no longer has, or gives it
// the wrong type: OLM would then fail to parse the initialization-resource.
// Strict decoding rejects unknown fields, so a typo'd component key ("dashbaord")
// fails here even when the drift check passes.
func TestInitResourceAnnotationIsValidDSC(t *testing.T) {
	root := repoRoot(t)

	csvBytes, err := os.ReadFile(filepath.Join(root, rhoaiCSVRelPath))
	require.NoError(t, err, "reading %s", rhoaiCSVRelPath)

	var csv struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	require.NoError(t, yaml.Unmarshal(csvBytes, &csv), "parsing %s", rhoaiCSVRelPath)

	annotation := csv.Metadata.Annotations[initResourceAnnotation]
	require.NotEmpty(t, annotation, "%s annotation missing from %s", initResourceAnnotation, rhoaiCSVRelPath)

	var dsc dscv2.DataScienceCluster
	require.NoError(t, yaml.UnmarshalStrict([]byte(annotation), &dsc),
		"the %s annotation must deserialize into a v2 DataScienceCluster with no unknown fields; "+
			"a field may be misspelled or renamed in the API (RHOAIENG-89419)", initResourceAnnotation)

	require.Equal(t, "DataScienceCluster", dsc.Kind, "annotation kind")
	require.Equal(t, "datasciencecluster.opendatahub.io/v2", dsc.APIVersion, "annotation apiVersion")
	require.Equal(t, "default-dsc", dsc.Name, "annotation metadata.name")
}

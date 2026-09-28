package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const (
	rhoaiCSVRelPath       = "config/rhoai/manifests/bases/rhods-operator.clusterserviceversion.yaml"
	rhoaiDSCSampleRelPath = "config/rhoai/samples/datasciencecluster_v2_datasciencecluster.yaml"

	initResourceAnnotation = "operatorframework.io/initialization-resource"
)

// TestInitResourceAnnotationMatchesSample guards against the RHOAIENG-89419
// regression. The hand-maintained operatorframework.io/initialization-resource
// annotation on the rhoai CSV drives the OLM "DataScienceCluster required"
// prompt, while the "Provided APIs" tab is driven by alm-examples, which is
// auto-generated from the DSC sample. The sample is the source of truth; the
// annotation must present the same default DataScienceCluster. Nothing
// regenerates the annotation, so when the two drift users see a different
// default DSC depending on how they create it -- only this test catches it.
func TestInitResourceAnnotationMatchesSample(t *testing.T) {
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

	// The annotation value is a JSON DataScienceCluster. sigs.k8s.io/yaml
	// parses JSON (a subset of YAML) and the sample YAML through the same
	// JSON codec, so both decode to identically typed maps -- a deep compare
	// then flags any drift, including future numeric or boolean fields.
	var annotationDSC map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(annotation), &annotationDSC),
		"parsing the %s annotation as a DataScienceCluster", initResourceAnnotation)

	sampleBytes, err := os.ReadFile(filepath.Join(root, rhoaiDSCSampleRelPath))
	require.NoError(t, err, "reading %s", rhoaiDSCSampleRelPath)

	var sampleDSC map[string]any
	require.NoError(t, yaml.Unmarshal(sampleBytes, &sampleDSC), "parsing %s", rhoaiDSCSampleRelPath)

	require.Equal(t, sampleDSC, annotationDSC,
		"the %s annotation in %s has drifted from %s; re-sync the annotation with the sample "+
			"(the source of truth) so the OLM 'DataScienceCluster required' prompt and the "+
			"'Provided APIs' tab present the same default DSC (RHOAIENG-89419)",
		initResourceAnnotation, rhoaiCSVRelPath, rhoaiDSCSampleRelPath)
}

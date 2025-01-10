//nolint:testpackage
package datasciencepipelines

import (
	"encoding/json"
	"testing"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1/datasciencepipelines"

	. "github.com/onsi/gomega"
)

func TestComputeParamsMap(t *testing.T) {
	g := NewWithT(t)

	dsp := componentApi.DataSciencePipelines{
		Spec: componentApi.DataSciencePipelinesSpec{
			DataSciencePipelinesCommonSpec: componentApi.DataSciencePipelinesCommonSpec{
				PreloadedPipelines: datasciencepipelines.PreloadedPipelinesSpec{},
			},
		},
	}

	result, err := computeParamsMap(&dsp)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(result).ShouldNot(BeEmpty())

	// Marshal the expected value for comparison
	expectedData, err := json.Marshal(dsp.Spec.PreloadedPipelines)
	g.Expect(err).ShouldNot(HaveOccurred())

	expectedData, err = json.Marshal(string(expectedData))
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(result).Should(HaveKeyWithValue(preinstalledPipelineParamsKey, string(expectedData)))
}

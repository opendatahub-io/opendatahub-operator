//nolint:testpackage
package datasciencepipelines

import (
	"encoding/json"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestComputeParamsMap(t *testing.T) {
	g := NewWithT(t)

	dsp := componentApi.DataSciencePipelines{
		Spec: componentApi.DataSciencePipelinesSpec{
			DataSciencePipelinesCommonSpec: componentApi.DataSciencePipelinesCommonSpec{
				PreloadedPipelines: datasciencepipelines.ManagedPipelinesSpec{},
			},
		},
	}

	v := semver.MustParse("1.2.3")
	rr := types.ReconciliationRequest{
		Instance: &dsp,
		Release: cluster.Release{
			Version: version.OperatorVersion{
				Version: v,
			},
		},
	}

	result, err := computeParamsMap(&rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(result).ShouldNot(BeEmpty())

	// Marshal the expected value for comparison
	expectedData, err := json.Marshal(dsp.Spec.PreloadedPipelines)
	g.Expect(err).ShouldNot(HaveOccurred())

	expectedData, err = json.Marshal(string(expectedData))
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(result).Should(And(
		HaveKeyWithValue(managedPipelineParamsKey, string(expectedData)),
	))
}

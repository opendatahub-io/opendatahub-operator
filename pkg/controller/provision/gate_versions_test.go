package provision_test

import (
	"testing"

	"github.com/blang/semver/v4"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
)

func TestResolveUpgradeGateVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		deployed string
		running  string
		csvs     []*operatorsv1alpha1.ClusterServiceVersion
		expected string
		err      bool
	}{
		{
			name:     "running release is used when only old CSV is visible",
			deployed: "2.25.10",
			running:  "3.5.1",
			csvs:     []*operatorsv1alpha1.ClusterServiceVersion{dscOwningCSV("rhods-operator.2.25.10", "2.25.10")},
			expected: "3.5.1",
		},
		{
			name:     "newer DSC-owning CSV protects against stale running release",
			deployed: "2.25.10",
			running:  "2.25.10",
			csvs: []*operatorsv1alpha1.ClusterServiceVersion{
				dscOwningCSV("rhods-operator.2.25.10", "2.25.10"),
				dscOwningCSV("rhods-operator.v3.5.2", "3.5.2"),
			},
			expected: "3.5.2",
		},
		{
			name:     "highest DSC-owning CSV wins when running release is unavailable",
			deployed: "2.25.10",
			csvs: []*operatorsv1alpha1.ClusterServiceVersion{
				dscOwningCSV("rhods-operator.v3.5.1", "3.5.1"),
				dscOwningCSV("rhods-operator.v3.5.3", "3.5.3"),
				dscOwningCSV("rhods-operator.v3.5.2", "3.5.2"),
			},
			expected: "3.5.3",
		},
		{
			name:     "non-owning CSVs are ignored",
			deployed: "2.25.10",
			running:  "3.5.1",
			csvs: []*operatorsv1alpha1.ClusterServiceVersion{
				csv("dependency.v3.5.2", "3.5.2", false),
				dscOwningCSV("rhods-operator.2.25.10", "2.25.10"),
			},
			expected: "3.5.1",
		},
		{
			name:     "missing target fails closed",
			deployed: "2.25.10",
			csvs:     []*operatorsv1alpha1.ClusterServiceVersion{dscOwningCSV("rhods-operator.2.25.10", "2.25.10")},
			err:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			require.NoError(t, operatorsv1alpha1.AddToScheme(scheme))

			objects := make([]runtime.Object, len(tc.csvs))
			for i := range tc.csvs {
				objects[i] = tc.csvs[i]
			}
			cli := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()

			targetVersion, err := provision.ResolveUpgradeGateVersion(
				t.Context(), cli, testNS,
				releaseForVersion(tc.deployed),
				releaseForVersion(tc.running),
			)

			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, targetVersion)
		})
	}
}

func releaseForVersion(value string) common.Release {
	if value == "" {
		return common.Release{}
	}
	return common.Release{Version: ofversion.OperatorVersion{Version: semver.MustParse(value)}}
}

func dscOwningCSV(name string, version string) *operatorsv1alpha1.ClusterServiceVersion {
	return csv(name, version, true)
}

func csv(name string, value string, ownsDSC bool) *operatorsv1alpha1.ClusterServiceVersion {
	parsed := semver.MustParse(value)
	result := &operatorsv1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNS},
		Spec: operatorsv1alpha1.ClusterServiceVersionSpec{
			Version: ofversion.OperatorVersion{Version: parsed},
		},
	}
	if ownsDSC {
		result.Spec.CustomResourceDefinitions.Owned = []operatorsv1alpha1.CRDDescription{{
			Name:    "datascienceclusters.datasciencecluster.opendatahub.io",
			Version: "v2",
			Kind:    "DataScienceCluster",
		}}
	}
	return result
}

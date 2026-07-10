package releases_test

import (
	"testing"

	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/blang/semver/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/releases"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestPlatformVersionAction(t *testing.T) {
	tests := []struct {
		name             string
		skipDeploy       bool
		existingReleases []common.ComponentRelease
		expectedReleases []common.ComponentRelease
	}{
		{
			name:             "should record platform version on fresh component",
			skipDeploy:       false,
			existingReleases: nil,
			expectedReleases: []common.ComponentRelease{
				{Name: "platform", Version: "2.20.0"},
			},
		},
		{
			name:       "should preserve existing releases and add platform version",
			skipDeploy: false,
			existingReleases: []common.ComponentRelease{
				{Name: "Kubeflow Pipelines", Version: "2.2.0", RepoURL: "https://github.com/kubeflow/kfp-tekton"},
			},
			expectedReleases: []common.ComponentRelease{
				{Name: "Kubeflow Pipelines", Version: "2.2.0", RepoURL: "https://github.com/kubeflow/kfp-tekton"},
				{Name: "platform", Version: "2.20.0"},
			},
		},
		{
			name:       "should update existing platform version",
			skipDeploy: false,
			existingReleases: []common.ComponentRelease{
				{Name: "Kubeflow Pipelines", Version: "2.2.0"},
				{Name: "platform", Version: "2.19.0"},
			},
			expectedReleases: []common.ComponentRelease{
				{Name: "Kubeflow Pipelines", Version: "2.2.0"},
				{Name: "platform", Version: "2.20.0"},
			},
		},
		{
			name:       "should not record platform version when SkipDeploy is true",
			skipDeploy: true,
			existingReleases: []common.ComponentRelease{
				{Name: "platform", Version: "2.19.0"},
			},
			expectedReleases: []common.ComponentRelease{
				{Name: "platform", Version: "2.19.0"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			instance := &componentApi.DataSciencePipelines{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-instance",
				},
			}

			// Set existing releases if provided
			if tt.existingReleases != nil {
				instance.SetReleaseStatus(tt.existingReleases)
			}

			rr := types.ReconciliationRequest{
				Instance:   instance,
				SkipDeploy: tt.skipDeploy,
				Release: common.Release{
					Version: version.OperatorVersion{Version: semver.MustParse("2.20.0")},
				},
			}

			action := releases.NewPlatformVersionAction()
			err := action(ctx, &rr)
			g.Expect(err).NotTo(HaveOccurred())

			wr := rr.Instance.(common.WithReleases)
			actualReleases := wr.GetReleaseStatus()
			g.Expect(*actualReleases).To(Equal(tt.expectedReleases))
		})
	}
}

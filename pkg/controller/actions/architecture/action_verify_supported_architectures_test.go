package architecture_test

import (
	"context"
	"testing"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/architecture"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestVerifySupportedArchitectures(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()
	dsci := &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}}
	release := common.Release{Name: cluster.OpenDataHub}

	cl, err := fakeclient.New(
		&corev1.Node{
			TypeMeta: metav1.TypeMeta{
				APIVersion: gvk.Node.GroupVersion().String(),
				Kind:       gvk.Node.GroupVersion().String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "amd64-node",
				Labels: map[string]string{
					labels.NodeArch: "amd64",
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		},
		&corev1.Node{
			TypeMeta: metav1.TypeMeta{
				APIVersion: gvk.Node.GroupVersion().String(),
				Kind:       gvk.Node.GroupVersion().String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "ppc64le-node",
				Labels: map[string]string{
					labels.NodeArch: "ppc64le",
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		},
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = architecture.VerifySupportedArchitectures(ctx, &types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.CodeFlare{
			ObjectMeta: metav1.ObjectMeta{
				Name: "codeflare-no-arch",
			},
			Status: componentApi.CodeFlareStatus{
				CodeFlareCommonStatus: componentApi.CodeFlareCommonStatus{
					ComponentReleaseStatus: common.ComponentReleaseStatus{
						Releases: []common.ComponentRelease{
							{
								Name:    "CodeFlare operator",
								Version: "1.15.0",
								RepoURL: "https://github.com/project-codeflare/codeflare-operator",
							},
						},
					},
				},
			},
		},
		DSCI:    dsci,
		Release: release,
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	err = architecture.VerifySupportedArchitectures(ctx, &types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.CodeFlare{
			ObjectMeta: metav1.ObjectMeta{
				Name: "codeflare-ppc64le",
			},
			Status: componentApi.CodeFlareStatus{
				CodeFlareCommonStatus: componentApi.CodeFlareCommonStatus{
					ComponentReleaseStatus: common.ComponentReleaseStatus{
						Releases: []common.ComponentRelease{
							{
								Name:    "CodeFlare operator",
								Version: "1.15.0",
								RepoURL: "https://github.com/project-codeflare/codeflare-operator",
								SupportedArchitectures: []string{
									"ppc64le",
								},
							},
						},
					},
				},
			},
		},
		DSCI:    dsci,
		Release: release,
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	err = architecture.VerifySupportedArchitectures(ctx, &types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.CodeFlare{
			ObjectMeta: metav1.ObjectMeta{
				Name: "codeflare-ppc64le",
			},
			Status: componentApi.CodeFlareStatus{
				CodeFlareCommonStatus: componentApi.CodeFlareCommonStatus{
					ComponentReleaseStatus: common.ComponentReleaseStatus{
						Releases: []common.ComponentRelease{
							{
								Name:    "CodeFlare operator",
								Version: "1.15.0",
								RepoURL: "https://github.com/project-codeflare/codeflare-operator",
								SupportedArchitectures: []string{
									"s390x",
								},
							},
						},
					},
				},
			},
		},
		DSCI:    dsci,
		Release: release,
	})
	g.Expect(err).Should(HaveOccurred())
}

func Test_hasCompatibleArchitecture(t *testing.T) {
	type args struct {
		supportedArches map[string]struct{}
		nodeArches      map[string]struct{}
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Common architecture exists",
			args: args{
				supportedArches: map[string]struct{}{
					"amd64":   {},
					"ppc64le": {},
				},
				nodeArches: map[string]struct{}{
					"amd64": {},
				},
			},
			want: true,
		},
		{
			name: "No common architecture exists",
			args: args{
				supportedArches: map[string]struct{}{
					"amd64":   {},
					"ppc64le": {},
				},
				nodeArches: map[string]struct{}{
					"s390x": {},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := architecture.HasCompatibleArchitecture(tt.args.supportedArches, tt.args.nodeArches); got != tt.want {
				t.Errorf("hasCompatibleArchitecture() = %v, want %v", got, tt.want)
			}
		})
	}
}

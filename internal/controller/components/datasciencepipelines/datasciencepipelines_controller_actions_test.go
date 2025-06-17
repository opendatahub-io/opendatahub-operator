//nolint:testpackage
package datasciencepipelines

import (
	"context"
	"os"
	"path"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestCheckPreConditions_ArgoWorkflowsRemoved(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dsp := &componentApi.DataSciencePipelines{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsp",
		},
		Spec: componentApi.DataSciencePipelinesSpec{
			DataSciencePipelinesCommonSpec: componentApi.DataSciencePipelinesCommonSpec{
				ArgoWorkflowsControllers: &componentApi.ArgoWorkflowsControllersSpec{
					ManagementState: operatorv1.Removed,
				},
			},
		},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsp,
		Conditions: conditions.NewManager(dsp, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(dsp).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionArgoWorkflowAvailable, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, status.ConditionArgoWorkflowAvailable, status.DataSciencePipelinesArgoWorkflowsNotManagedReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, status.ConditionArgoWorkflowAvailable, status.DataSciencePipelinesArgoWorkflowsNotManagedMessage),
		)),
	)
}

func TestCheckPreConditions_ArgoWorkflowsManaged(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dsp := &componentApi.DataSciencePipelines{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsp",
		},
		Spec: componentApi.DataSciencePipelinesSpec{
			DataSciencePipelinesCommonSpec: componentApi.DataSciencePipelinesCommonSpec{
				ArgoWorkflowsControllers: &componentApi.ArgoWorkflowsControllersSpec{
					ManagementState: operatorv1.Managed,
				},
			},
		},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   dsp,
		Conditions: conditions.NewManager(dsp, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(dsp).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionArgoWorkflowAvailable, metav1.ConditionTrue),
		)),
	)
}

func TestArgoWorkflowsControllersOptions(t *testing.T) {
	oldDeployPath := odhdeploy.DefaultManifestPath
	odhdeploy.DefaultManifestPath = t.TempDir()
	defer func() {
		odhdeploy.DefaultManifestPath = oldDeployPath
	}()

	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()

	// Create the base directory structure
	baseDir := path.Join(odhdeploy.DefaultManifestPath, ComponentName, "base")
	err := os.MkdirAll(baseDir, 0o755)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create a mock params.env file
	paramsPath := path.Join(baseDir, "params.env")
	err = os.WriteFile(paramsPath, []byte("key1=value1\nkey2=value2\n"), 0o600)
	g.Expect(err).ShouldNot(HaveOccurred())

	tests := []struct {
		name                                   string
		instance                               common.PlatformObject
		expectedError                          bool
		expectedArgoWorkflowsControllersParams bool
	}{
		{
			name: "successfully update params.env with default values",
			instance: &componentApi.DataSciencePipelines{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-dsp",
				},
				Spec: componentApi.DataSciencePipelinesSpec{
					DataSciencePipelinesCommonSpec: componentApi.DataSciencePipelinesCommonSpec{
						DevFlagsSpec: common.DevFlagsSpec{},
					},
				},
			},
			expectedError:                          false,
			expectedArgoWorkflowsControllersParams: false,
		},
		{
			name: "successfully update params.env with unmanaged Argo Workflows controllers",
			instance: &componentApi.DataSciencePipelines{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-dsp",
				},
				Spec: componentApi.DataSciencePipelinesSpec{
					DataSciencePipelinesCommonSpec: componentApi.DataSciencePipelinesCommonSpec{
						ArgoWorkflowsControllers: &componentApi.ArgoWorkflowsControllersSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
			},
			expectedError:                          false,
			expectedArgoWorkflowsControllersParams: true,
		},
		{
			name: "error when instance is not DataSciencePipelines",
			instance: &componentApi.Dashboard{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-dashboard",
				},
			},
			expectedError:                          true,
			expectedArgoWorkflowsControllersParams: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli, err := fakeclient.New()
			g.Expect(err).ShouldNot(HaveOccurred())

			rr := types.ReconciliationRequest{
				Client:     cli,
				Instance:   tt.instance,
				Conditions: conditions.NewManager(tt.instance, "Ready"),
				DSCI: &dsciv1.DSCInitialization{
					Spec: dsciv1.DSCInitializationSpec{
						ApplicationsNamespace: ns,
					},
				},
				Release: common.Release{Name: cluster.OpenDataHub},
			}

			err = argoWorkflowsControllersOptions(ctx, &rr)

			if tt.expectedError {
				g.Expect(err).Should(HaveOccurred())
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())

				paramsPath := path.Join(odhdeploy.DefaultManifestPath, ComponentName, "base", "params.env")
				content, err := os.ReadFile(paramsPath)
				g.Expect(err).ShouldNot(HaveOccurred())

				if tt.expectedArgoWorkflowsControllersParams {
					g.Expect(string(content)).Should(ContainSubstring(`ARGOWORKFLOWSCONTROLLERS={"managementState":"Removed"}`))
				} else {
					g.Expect(string(content)).Should(ContainSubstring(`ARGOWORKFLOWSCONTROLLERS={"managementState":"Managed"}`))
				}
			}
		})
	}
}

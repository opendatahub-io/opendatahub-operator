//nolint:testpackage
package datasciencepipelines

import (
	"os"
	"path"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestCheckPreConditions(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	tests := []struct {
		name                    string
		setupClient             func() client.Client
		instance                *componentApi.DataSciencePipelines
		expectedError           error
		expectedConditionStatus metav1.ConditionStatus
		expectedReason          string
		expectedMessage         string
	}{
		{
			name: "ArgoWorkflowsRemoved_CRDNotFound",
			setupClient: func() client.Client {
				cli, err := fakeclient.New()
				g.Expect(err).ShouldNot(HaveOccurred())
				return cli
			},
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
			expectedError:           ErrArgoWorkflowCRDMissing,
			expectedConditionStatus: metav1.ConditionFalse,
			expectedReason:          status.DataSciencePipelinesArgoWorkflowsCRDMissingReason,
			expectedMessage:         status.DataSciencePipelinesArgoWorkflowsCRDMissingMessage,
		},
		{
			name: "ArgoWorkflowsRemoved_CRDExists",
			setupClient: func() client.Client {
				cli, err := fakeclient.New()
				g.Expect(err).ShouldNot(HaveOccurred())

				crd := &extv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{
						Name: ArgoWorkflowCRD,
					},
					Spec: extv1.CustomResourceDefinitionSpec{
						Group: "argoproj.io",
						Names: extv1.CustomResourceDefinitionNames{
							Kind: "Workflow",
						},
					},
				}
				err = cli.Create(ctx, crd)
				g.Expect(err).ShouldNot(HaveOccurred())
				return cli
			},
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
			expectedError:           nil,
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          status.DataSciencePipelinesArgoWorkflowsNotManagedReason,
			expectedMessage:         status.DataSciencePipelinesArgoWorkflowsNotManagedMessage,
		},
		{
			name: "ArgoWorkflowsManaged_CRDNotFound",
			setupClient: func() client.Client {
				cli, err := fakeclient.New()
				g.Expect(err).ShouldNot(HaveOccurred())
				return cli
			},
			instance: &componentApi.DataSciencePipelines{
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
			},
			expectedError:           nil,
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          "",
			expectedMessage:         "",
		},
		{
			name: "ArgoWorkflowsManaged_CRDOwnedByODH",
			setupClient: func() client.Client {
				cli, err := fakeclient.New()
				g.Expect(err).ShouldNot(HaveOccurred())

				// Create a CRD with ODH label
				crd := &extv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{
						Name: ArgoWorkflowCRD,
						Labels: map[string]string{
							labels.ODH.Component(LegacyComponentName): "true",
						},
					},
					Spec: extv1.CustomResourceDefinitionSpec{
						Group: "argoproj.io",
						Names: extv1.CustomResourceDefinitionNames{
							Kind: "Workflow",
						},
					},
				}
				err = cli.Create(ctx, crd)
				g.Expect(err).ShouldNot(HaveOccurred())
				return cli
			},
			instance: &componentApi.DataSciencePipelines{
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
			},
			expectedError:           nil,
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          "",
			expectedMessage:         "",
		},
		{
			name: "ArgoWorkflowsManaged_CRDNotOwnedByODH",
			setupClient: func() client.Client {
				cli, err := fakeclient.New()
				g.Expect(err).ShouldNot(HaveOccurred())

				// Create a CRD without ODH label
				crd := &extv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{
						Name: ArgoWorkflowCRD,
						Labels: map[string]string{
							"some-other-label": "value",
						},
					},
					Spec: extv1.CustomResourceDefinitionSpec{
						Group: "argoproj.io",
						Names: extv1.CustomResourceDefinitionNames{
							Kind: "Workflow",
						},
					},
				}
				err = cli.Create(ctx, crd)
				g.Expect(err).ShouldNot(HaveOccurred())
				return cli
			},
			instance: &componentApi.DataSciencePipelines{
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
			},
			expectedError:           ErrArgoWorkflowAPINotOwned,
			expectedConditionStatus: metav1.ConditionFalse,
			expectedReason:          status.DataSciencePipelinesDoesntOwnArgoCRDReason,
			expectedMessage:         status.DataSciencePipelinesDoesntOwnArgoCRDMessage,
		},
		{
			name: "ArgoWorkflowsNotSpecified",
			setupClient: func() client.Client {
				cli, err := fakeclient.New()
				g.Expect(err).ShouldNot(HaveOccurred())
				return cli
			},
			instance: &componentApi.DataSciencePipelines{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-dsp",
				},
				Spec: componentApi.DataSciencePipelinesSpec{
					DataSciencePipelinesCommonSpec: componentApi.DataSciencePipelinesCommonSpec{},
				},
			},
			expectedError:           nil,
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          "",
			expectedMessage:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli := tt.setupClient()

			rr := types.ReconciliationRequest{
				Client:     cli,
				Instance:   tt.instance,
				Conditions: conditions.NewManager(tt.instance, status.ConditionTypeReady),
			}

			err := checkPreConditions(ctx, &rr)

			if tt.expectedError != nil {
				g.Expect(err).Should(Equal(tt.expectedError))
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())
			}

			// Check condition status
			g.Expect(tt.instance).Should(
				WithTransform(resources.ToUnstructured, And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionArgoWorkflowAvailable, tt.expectedConditionStatus),
				)),
			)

			// Check reason and message if specified
			if tt.expectedReason != "" {
				g.Expect(tt.instance).Should(
					WithTransform(resources.ToUnstructured, And(
						jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, status.ConditionArgoWorkflowAvailable, tt.expectedReason),
					)),
				)
			}

			if tt.expectedMessage != "" {
				g.Expect(tt.instance).Should(
					WithTransform(resources.ToUnstructured, And(
						jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, status.ConditionArgoWorkflowAvailable, tt.expectedMessage),
					)),
				)
			}
		})
	}
}

func TestCheckPreConditions_WrongInstanceType(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	wrongInstance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dashboard",
		},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   wrongInstance,
		Conditions: conditions.NewManager(wrongInstance, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("is not a componentApi.DataSciencePipelines"))
}

func TestArgoWorkflowsControllersOptions(t *testing.T) {
	g := NewWithT(t)

	oldDeployPath := odhdeploy.DefaultManifestPath
	defer func() {
		odhdeploy.DefaultManifestPath = oldDeployPath
	}()

	ctx := t.Context()

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
			odhdeploy.DefaultManifestPath = t.TempDir()

			// Create the base directory structure
			baseDir := path.Join(odhdeploy.DefaultManifestPath, ComponentName, "base")
			err := os.MkdirAll(baseDir, 0o755)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Create a mock params.env file
			paramsPath := path.Join(baseDir, "params.env")
			err = os.WriteFile(paramsPath, []byte("key1=value1\nkey2=value2\n"), 0o600)
			g.Expect(err).ShouldNot(HaveOccurred())

			cli, err := fakeclient.New()
			g.Expect(err).ShouldNot(HaveOccurred())

			rr := types.ReconciliationRequest{
				Client:     cli,
				Instance:   tt.instance,
				Conditions: conditions.NewManager(tt.instance, "Ready"),
				DSCI: &dsciv2.DSCInitialization{
					Spec: dsciv2.DSCInitializationSpec{
						ApplicationsNamespace: "test-namespace",
					},
				},
				Release: common.Release{Name: cluster.OpenDataHub},
			}

			err = argoWorkflowsControllersOptions(ctx, &rr)

			if tt.expectedError {
				g.Expect(err).Should(HaveOccurred())
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())

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

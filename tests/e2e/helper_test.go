package e2e_test

import (
	"context"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofapiv1 "github.com/operator-framework/api/pkg/operators/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	knativeServingNamespace  = "knative-serving"
	serverlessOpName         = "serverless-operator"
	ownedNamespaceNumber     = 1 // set to 4 for RHOAI
	deleteConfigMap          = "delete-configmap-name"
	operatorReadyTimeout     = 2 * time.Minute
	componentReadyTimeout    = 7 * time.Minute // in component code is to set 2-3 mins, keep it 7 mins just the same value we used after introduce "Ready" check
	componentDeletionTimeout = 1 * time.Minute
	crdReadyTimeout          = 1 * time.Minute
	csvWaitTimeout           = 1 * time.Minute
	dsciCreationTimeout      = 20 * time.Second // time required to get a DSCI is created.
	dscCreationTimeout       = 20 * time.Second // time required to wait till DSC is created.
	generalRetryInterval     = 10 * time.Second
	generalWaitTimeout       = 2 * time.Minute
	generalPollInterval      = 1 * time.Second
	readyStatus              = "Ready"
	dscKind                  = "DataScienceCluster"
)

func (tc *testContext) waitForOperatorDeployment(name string, replicas int32) error {
	err := wait.PollUntilContextTimeout(tc.ctx, generalRetryInterval, operatorReadyTimeout, false, func(ctx context.Context) (bool, error) {
		controllerDeployment, err := tc.kubeClient.AppsV1().Deployments(tc.operatorNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if k8serr.IsNotFound(err) {
				return false, nil
			}
			log.Printf("Failed to get %s controller deployment", name)
			return false, err
		}

		for _, condition := range controllerDeployment.Status.Conditions {
			if condition.Type == appsv1.DeploymentAvailable {
				if condition.Status == corev1.ConditionTrue && controllerDeployment.Status.ReadyReplicas == replicas {
					return true, nil
				}
			}
		}
		log.Printf("Error in %s deployment", name)
		return false, nil
	})
	return err
}

func (tc *testContext) getComponentDeployments(componentGVK schema.GroupVersionKind) ([]appsv1.Deployment, error) {
	deployments := appsv1.DeploymentList{}
	err := tc.customClient.List(
		tc.ctx,
		&deployments,
		client.InNamespace(
			tc.applicationsNamespace,
		),
		client.MatchingLabels{
			labels.PlatformPartOf: strings.ToLower(componentGVK.Kind),
		},
	)

	if err != nil {
		return nil, err
	}

	return deployments.Items, nil
}

func setupDSCICR(name string) *dsciv1.DSCInitialization {
	dsciTest := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: "redhat-ods-applications",
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: "redhat-ods-monitoring",
				},
			},
			TrustedCABundle: &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
				CustomCABundle:  "",
			},
			ServiceMesh: &infrav1.ServiceMeshSpec{
				ControlPlane: infrav1.ControlPlaneSpec{
					MetricsCollection: "Istio",
					Name:              "data-science-smcp",
					Namespace:         "istio-system",
				},
				ManagementState: operatorv1.Managed,
			},
		},
	}
	return dsciTest
}

func setupDSCInstance(name string) *dscv1.DataScienceCluster {
	dscTest := &dscv1.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				// keep dashboard as enabled, because other test is rely on this
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				ModelMeshServing: componentApi.DSCModelMeshServing{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				DataSciencePipelines: componentApi.DSCDataSciencePipelines{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						DefaultDeploymentMode: componentApi.Serverless,
						Serving: infrav1.ServingSpec{
							ManagementState: operatorv1.Managed,
							Name:            "knative-serving",
							IngressGateway: infrav1.GatewaySpec{
								Certificate: infrav1.CertificateSpec{
									Type: infrav1.OpenshiftDefaultIngress,
								},
							},
						},
					},
				},
				CodeFlare: componentApi.DSCCodeFlare{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Ray: componentApi.DSCRay{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Kueue: componentApi.DSCKueue{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				TrustyAI: componentApi.DSCTrustyAI{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				ModelRegistry: componentApi.DSCModelRegistry{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
						RegistriesNamespace: modelregistryctrl.DefaultModelRegistriesNamespace,
					},
				},
				TrainingOperator: componentApi.DSCTrainingOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				FeastOperator: componentApi.DSCFeastOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}
	return dscTest
}

func setupSubscription(name string, ns string) *ofapi.Subscription {
	return &ofapi.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: &ofapi.SubscriptionSpec{
			CatalogSource:          "redhat-operators",
			CatalogSourceNamespace: "openshift-marketplace",
			Channel:                "stable",
			Package:                name,
			InstallPlanApproval:    ofapi.ApprovalAutomatic,
		},
	}
}

func (tc *testContext) validateCRD(crdName string) error {
	crd := &apiextv1.CustomResourceDefinition{}
	obj := client.ObjectKey{
		Name: crdName,
	}

	err := wait.PollUntilContextTimeout(tc.ctx, generalRetryInterval, crdReadyTimeout, false, func(ctx context.Context) (bool, error) {
		err := tc.customClient.Get(ctx, obj, crd)
		if err != nil {
			if k8serr.IsNotFound(err) {
				return false, nil
			}
			log.Printf("Failed to get CRD %s", crdName)
			return false, err
		}

		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextv1.Established {
				if condition.Status == apiextv1.ConditionTrue {
					return true, nil
				}
			}
		}
		log.Printf("Error to get CRD %s condition's matching", crdName)
		return false, nil
	})
	return err
}

func (tc *testContext) wait(isReady func(ctx context.Context) (bool, error)) error {
	return wait.PollUntilContextTimeout(tc.ctx, generalRetryInterval, generalWaitTimeout, true, isReady)
}

func getCSV(ctx context.Context, cli client.Client, name string, namespace string) (*ofapi.ClusterServiceVersion, error) {
	isMatched := func(csv *ofapi.ClusterServiceVersion, name string) bool {
		return strings.Contains(csv.ObjectMeta.Name, name)
	}

	opt := &client.ListOptions{
		Namespace: namespace,
	}
	csvList := &ofapi.ClusterServiceVersionList{}
	err := cli.List(ctx, csvList, opt)
	if err != nil {
		return nil, err
	}

	// do not use range Items to avoid pointer to the loop variable
	for i := range len(csvList.Items) {
		csv := &csvList.Items[i]
		if isMatched(csv, name) {
			return csv, nil
		}
	}

	return nil, k8serr.NewNotFound(schema.GroupResource{}, name)
}

// Use existing or create a new one.
func getSubscription(tc *testContext, name string, ns string) (*ofapi.Subscription, error) {
	createSubscription := func(name string, ns string) (*ofapi.Subscription, error) {
		// this just creates a manifest
		sub := setupSubscription(name, ns)

		if err := tc.customClient.Create(tc.ctx, sub); err != nil {
			return nil, fmt.Errorf("error creating subscription: %w", err)
		}

		return sub, nil
	}

	sub := &ofapi.Subscription{}
	key := types.NamespacedName{
		Namespace: ns,
		Name:      name,
	}

	err := tc.customClient.Get(tc.ctx, key, sub)
	if k8serr.IsNotFound(err) {
		return createSubscription(name, ns)
	}
	if err != nil {
		return nil, fmt.Errorf("error getting subscription: %w", err)
	}

	return sub, nil
}

func waitCSV(tc *testContext, name string, ns string) error {
	interval := generalRetryInterval
	isReady := func(ctx context.Context) (bool, error) {
		csv, err := getCSV(ctx, tc.customClient, name, ns)
		if k8serr.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}

		return csv.Status.Phase == "Succeeded", nil
	}

	err := wait.PollUntilContextTimeout(tc.ctx, interval, csvWaitTimeout, false, isReady)
	if err != nil {
		return fmt.Errorf("Error installing %s CSV: %w", name, err)
	}

	return nil
}

func getInstallPlanName(tc *testContext, name string, ns string) (string, error) {
	sub := &ofapi.Subscription{}

	// waits for InstallPlanRef and copies value out of the closure
	err := tc.wait(func(ctx context.Context) (bool, error) {
		_sub, err := getSubscription(tc, name, ns)
		if err != nil {
			return false, err
		}
		*sub = *_sub
		return sub.Status.InstallPlanRef != nil, nil
	})

	if err != nil {
		return "", fmt.Errorf("Error creating subscription %s: %w", name, err)
	}

	return sub.Status.InstallPlanRef.Name, nil
}

func getInstallPlan(tc *testContext, name string, ns string) (*ofapi.InstallPlan, error) {
	// it creates subscription under the hood if needed and waits for InstallPlan reference
	planName, err := getInstallPlanName(tc, name, ns)
	if err != nil {
		return nil, err
	}

	obj := &ofapi.InstallPlan{}
	key := types.NamespacedName{
		Namespace: ns,
		Name:      planName,
	}

	err = tc.customClient.Get(tc.ctx, key, obj)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func approveInstallPlan(tc *testContext, plan *ofapi.InstallPlan) error {
	obj := &ofapi.InstallPlan{
		TypeMeta: metav1.TypeMeta{
			Kind:       "InstallPlan",
			APIVersion: "operators.coreos.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      plan.ObjectMeta.Name,
			Namespace: plan.ObjectMeta.Namespace,
		},
		Spec: ofapi.InstallPlanSpec{
			Approved:                   true,
			Approval:                   ofapi.ApprovalAutomatic,
			ClusterServiceVersionNames: plan.Spec.ClusterServiceVersionNames,
		},
	}
	force := true
	opt := &client.PatchOptions{
		FieldManager: "e2e-test-dsc",
		Force:        &force,
	}

	err := tc.customClient.Patch(tc.ctx, obj, client.Apply, opt)
	if err != nil {
		return fmt.Errorf("Error patching InstallPlan %s: %w", obj.ObjectMeta.Name, err)
	}

	return nil
}

func ensureOperatorNamespace(tc *testContext, name, ns string) error {
	operatorNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ns,
		},
	}
	foundNamespace := &corev1.Namespace{}
	err := tc.customClient.Get(tc.ctx, client.ObjectKeyFromObject(operatorNS), foundNamespace)
	if k8serr.IsNotFound(err) {
		if err := tc.customClient.Create(tc.ctx, operatorNS); err != nil {
			return fmt.Errorf("error create dependent operator namespace: %w", err)
		}
		// Just create it since namespace was not even there, and do not set spec with targetnamespaces!
		operatorGroup := &ofapiv1.OperatorGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
		}
		if err := tc.customClient.Create(tc.ctx, operatorGroup); err != nil {
			return fmt.Errorf("error create operatorgroup %s: %w", name, err)
		}
		return nil
	}
	return err
}

// 1. Ensure namespace exists.
// 2. Ensure Subscription exists.
// 3. Ensure InstallPlan exists.
// 4. InstallPlan to Automatic.
// 5. Wait for CSV.
func ensureOperator(tc *testContext, name string, ns string) error {
	// check namespace first if not exsit then create it along with OG
	if err := ensureOperatorNamespace(tc, name, ns); err != nil {
		return err
	}
	// it creates subscription under the hood if needed
	plan, err := getInstallPlan(tc, name, ns)
	if err != nil {
		return err
	}

	// in CI InstallPlan is in Manual mode
	if !plan.Spec.Approved {
		err = approveInstallPlan(tc, plan)
		if err != nil {
			return err
		}
	}

	return waitCSV(tc, name, ns)
}

func ensureServicemeshOperators(t *testing.T, tc *testContext) error { //nolint: thelper
	depOperators := map[string]string{
		"serverless-operator": "openshift-serverless",
		"servicemeshoperator": "openshift-operators",
		"authorino-operator":  "openshift-operators",
	}

	var errors *multierror.Error
	c := make(chan error)

	for name, ns := range depOperators {
		t.Logf("Ensuring %s is installed", name)
		go func(name, ns string) {
			err := ensureOperator(tc, name, ns)
			c <- err
		}(name, ns)
	}
	for range depOperators {
		err := <-c
		errors = multierror.Append(errors, err)
	}
	return errors.ErrorOrNil()
}

func (tc *testContext) setUp(t *testing.T) error { //nolint: thelper
	return ensureServicemeshOperators(t, tc)
}

func mockCRDcreation(group, version, kind, componentName string) *apiextv1.CustomResourceDefinition {
	return &apiextv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: strings.ToLower(fmt.Sprintf("%ss.%s", kind, group)),
			Labels: map[string]string{
				labels.ODH.Component(componentName): labels.True,
			},
		},
		Spec: apiextv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apiextv1.CustomResourceDefinitionNames{
				Kind:   kind,
				Plural: strings.ToLower(kind) + "s",
			},
			Scope: apiextv1.ClusterScoped,
			Versions: []apiextv1.CustomResourceDefinitionVersion{
				{
					Name:    version,
					Served:  true,
					Storage: true,
					Schema: &apiextv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
			},
		},
	}
}

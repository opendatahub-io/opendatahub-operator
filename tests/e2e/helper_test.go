package e2e_test

import (
	"context"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trainingoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
)

const (
	servicemeshNamespace = "openshift-operators"
	servicemeshOpName    = "servicemeshoperator"
	serverlessOpName     = "serverless-operator"
)

func (tc *testContext) waitForControllerDeployment(name string, replicas int32) error {
	err := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (bool, error) {
		controllerDeployment, err := tc.kubeClient.AppsV1().Deployments(tc.operatorNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
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

func setupDSCICR(name string) *dsciv1.DSCInitialization {
	dsciTest := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: "opendatahub",
			Monitoring: dsciv1.Monitoring{
				ManagementState: "Managed",
				Namespace:       "opendatahub",
			},
			TrustedCABundle: &dsciv1.TrustedCABundleSpec{
				ManagementState: "Managed",
				CustomCABundle:  "",
			},
			ServiceMesh: &infrav1.ServiceMeshSpec{
				ControlPlane: infrav1.ControlPlaneSpec{
					MetricsCollection: "Istio",
					Name:              "data-science-smcp",
					Namespace:         "istio-system",
				},
				ManagementState: "Managed",
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
				Dashboard: dashboard.Dashboard{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				Workbenches: workbenches.Workbenches{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				ModelMeshServing: modelmeshserving.ModelMeshServing{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				DataSciencePipelines: datasciencepipelines.DataSciencePipelines{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				Kserve: kserve.Kserve{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
					Serving: infrav1.ServingSpec{
						ManagementState: operatorv1.Managed,
					},
				},
				CodeFlare: codeflare.CodeFlare{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				Ray: ray.Ray{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				Kueue: kueue.Kueue{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				TrustyAI: trustyai.TrustyAI{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				ModelRegistry: modelregistry.ModelRegistry{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				TrainingOperator: trainingoperator.TrainingOperator{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
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
	err := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (bool, error) {
		err := tc.customClient.Get(ctx, obj, crd)
		if err != nil {
			if errors.IsNotFound(err) {
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
	return wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, true, isReady)
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
	for i := 0; i < len(csvList.Items); i++ {
		csv := &csvList.Items[i]
		if isMatched(csv, name) {
			return csv, nil
		}
	}

	return nil, errors.NewNotFound(schema.GroupResource{}, name)
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
	if errors.IsNotFound(err) {
		return createSubscription(name, ns)
	}
	if err != nil {
		return nil, fmt.Errorf("error getting subscription: %w", err)
	}

	return sub, nil
}

func waitCSV(tc *testContext, name string, ns string) error {
	interval := tc.resourceRetryInterval
	timeout := tc.resourceCreationTimeout * 3 // just empirical value

	isReady := func(ctx context.Context) (bool, error) {
		csv, err := getCSV(ctx, tc.customClient, name, ns)
		if errors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}

		return csv.Status.Phase == "Succeeded", nil
	}

	err := wait.PollUntilContextTimeout(tc.ctx, interval, timeout, false, isReady)
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
		FieldManager: "e2e-test",
		Force:        &force,
	}

	err := tc.customClient.Patch(tc.ctx, obj, client.Apply, opt)
	if err != nil {
		return fmt.Errorf("Error patching InstallPlan %s: %w", obj.ObjectMeta.Name, err)
	}

	return nil
}

func ensureOperator(tc *testContext, name string, ns string) error {
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
	ops := []string{
		serverlessOpName,
		servicemeshOpName,
	}
	var errors *multierror.Error
	c := make(chan error)

	for _, op := range ops {
		op := op // to avoid loop variable in the closures
		t.Logf("Ensuring %s is installed", op)
		go func(op string) {
			err := ensureOperator(tc, op, servicemeshNamespace)
			c <- err
		}(op)
	}

	for i := 0; i < len(ops); i++ {
		err := <-c
		errors = multierror.Append(errors, err)
	}

	return errors.ErrorOrNil()
}

func (tc *testContext) setUp(t *testing.T) error { //nolint: thelper
	return ensureServicemeshOperators(t, tc)
}

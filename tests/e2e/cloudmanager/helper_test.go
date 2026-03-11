package cloudmanager_test

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type deploymentRef struct {
	Name      string
	Namespace string
}

var managedDependencyDeployments = []deploymentRef{
	{Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator"},
	{Name: "openshift-lws-operator", Namespace: "openshift-lws-operator"},
	{Name: "servicemesh-operator3", Namespace: "istio-system"},
}

func newCloudManagerCR(deps map[string]any) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(provider.GVK)
	obj.SetName(provider.InstanceName)
	obj.Object["spec"] = map[string]any{
		"dependencies": deps,
	}
	return obj
}

func allManaged() map[string]any {
	return map[string]any{
		"certManager":  map[string]any{"managementPolicy": "Managed"},
		"lws":          map[string]any{"managementPolicy": "Managed"},
		"sailOperator": map[string]any{"managementPolicy": "Managed"},
	}
}

func crNN() types.NamespacedName {
	return types.NamespacedName{Name: provider.InstanceName}
}

// createCR creates the cloud manager CR with the given dependencies and registers
// cleanup to delete the CR and wait for managed deployments to be garbage collected.
func createCR(t *testing.T, wt *testf.WithT, deps map[string]any) {
	t.Helper()

	cr := newCloudManagerCR(deps)
	wt.Create(cr, crNN()).Eventually().Should(Not(BeNil()))

	t.Cleanup(func() {
		wt.Delete(provider.GVK, crNN()).Eventually().Should(Succeed())
		wt.Get(provider.GVK, crNN()).Eventually().Should(BeNil())

		for _, dep := range managedDependencyDeployments {
			wt.Get(gvk.Deployment, types.NamespacedName{
				Name: dep.Name, Namespace: dep.Namespace,
			}).Eventually().Should(BeNil())
		}
	})
}

// waitForReady waits until the CR has Ready=True in its status conditions.
func waitForReady(wt *testf.WithT) {
	wt.Get(provider.GVK, crNN()).Eventually().Should(
		jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
	)
}

// waitForDeploymentsAvailable waits until all managed dependency deployments
// have the Available=True condition, meaning they're actually running.
func waitForDeploymentsAvailable(wt *testf.WithT) {
	for _, dep := range managedDependencyDeployments {
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: dep.Name, Namespace: dep.Namespace,
		}).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`),
		)
	}
}

// consistentlyGone asserts that a deployment stays absent for a reasonable duration,
// confirming the controller is not recreating it.
func consistentlyGone(wt *testf.WithT, nn types.NamespacedName) {
	wt.Get(gvk.Deployment, nn).
		Consistently().
		WithTimeout(30 * time.Second).
		WithPolling(5 * time.Second).
		Should(BeNil())
}

// dependencyNamespaces lists the namespaces that the pre-apply hooks should create.
var dependencyNamespaces = []string{
	"cert-manager-operator",
	"openshift-lws-operator",
	"istio-system",
}

// GVKs for operand CRs not defined in the gvk package.
var (
	gvkIstio = schema.GroupVersionKind{
		Group:   "sailoperator.io",
		Version: "v1",
		Kind:    "Istio",
	}
)

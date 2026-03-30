package cloudmanager_test

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	ccmapi "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
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
	managed := string(ccmapi.Managed)
	return map[string]any{
		"certManager":  map[string]any{"managementPolicy": managed},
		"lws":          map[string]any{"managementPolicy": managed},
		"sailOperator": map[string]any{"managementPolicy": managed},
		"gatewayAPI":   map[string]any{"managementPolicy": managed},
	}
}

func k8sEngineCrNn() types.NamespacedName {
	return types.NamespacedName{Name: provider.InstanceName}
}

// waitForReady waits until the CR has Ready=True in its status conditions.
func waitForReady(wt *testf.WithT) {
	wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(
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

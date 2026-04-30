package cloudmanager_test

import (
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ccmapi "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
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

// certManagerOperandDeployments lists the cert-manager operand Deployments created
// by cert-manager-operator when it processes the CertManager/cluster CR. These are
// not directly owned by the *Engine CRs (no OwnerRef), so they do not appear in
// managedDependencyDeployments. They are cleaned up transitively: the CCM finalizer
// action deletes CertManager/cluster, cert-manager-operator processes its own
// finalizers, and removes these Deployments before the operator pod is killed.
var certManagerOperandDeployments = []deploymentRef{
	{Name: "cert-manager", Namespace: "cert-manager"},
	{Name: "cert-manager-cainjector", Namespace: "cert-manager"},
	{Name: "cert-manager-webhook", Namespace: "cert-manager"},
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

// pkiNames holds cert-manager PKI resource names discovered from the cloud manager deployment.
type pkiNames struct {
	IssuerName           string
	CertName             string
	CAIssuerName         string
	CertManagerNamespace string
}

// discoverPKIConfig reads the RHAI_* env vars from the cloud manager deployment
// on the cluster to determine the cert-manager PKI resource names. Falls back to
// opendatahub-prefixed defaults if the deployment is not found or env vars are absent.
func discoverPKIConfig(tc *testf.TestContext, providerName string) (pkiNames, error) {
	defaults := certmanager.DefaultBootstrapConfig()
	cfg := pkiNames{
		IssuerName:           defaults.IssuerName,
		CertName:             defaults.CertName,
		CAIssuerName:         defaults.CAIssuerName,
		CertManagerNamespace: defaults.CertManagerNamespace,
	}

	deployName := providerName + "-cloud-manager-operator"

	deployList := &appsv1.DeploymentList{}
	if err := tc.Client().List(tc.Context(), deployList, client.MatchingLabels{"name": deployName}); err != nil {
		return cfg, fmt.Errorf("failed to list deployments: %w", err)
	}

	if len(deployList.Items) == 0 {
		return cfg, nil
	}

	envVars := deployList.Items[0].Spec.Template.Spec.Containers[0].Env
	envMap := make(map[string]string, len(envVars))
	for _, e := range envVars {
		envMap[e.Name] = e.Value
	}

	if v, ok := envMap[certmanager.EnvCertName]; ok && v != "" {
		cfg.CertName = v
	}
	if v, ok := envMap[certmanager.EnvCAIssuerName]; ok && v != "" {
		cfg.CAIssuerName = v
	}
	if v, ok := envMap[certmanager.EnvCertManagerNS]; ok && v != "" {
		cfg.CertManagerNamespace = v
	}

	return cfg, nil
}

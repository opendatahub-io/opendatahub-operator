package cloudmanager_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ccmapi "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// Custom namespace names for testing non-default configurations.
const (
	customLWSOperatorNS  = "test-lws-operator"
	customSailOperatorNS = "test-istio-system"
)

// Hardcoded cert-manager namespaces.
const (
	certManagerOperatorNS = "cert-manager-operator"
	certManagerOperandNS  = "cert-manager"
)

type deploymentRef struct {
	Name      string
	Namespace string
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

func depsWithCustomNamespaces(policy ccmapi.ManagementPolicy) map[string]any {
	p := string(policy)
	return map[string]any{
		"certManager": map[string]any{
			"managementPolicy": p,
		},
		"lws": map[string]any{
			"managementPolicy": p,
			"configuration": map[string]any{
				"namespace": customLWSOperatorNS,
			},
		},
		"sailOperator": map[string]any{
			"managementPolicy": p,
			"configuration": map[string]any{
				"namespace": customSailOperatorNS,
			},
		},
		"gatewayAPI": map[string]any{
			"managementPolicy": p,
		},
	}
}

func k8sEngineCrNn() types.NamespacedName {
	return types.NamespacedName{Name: provider.InstanceName}
}

// waitForReady waits until the CR has Ready=True in its status conditions.
func waitForReady(wt *testf.WithT) {
	wt.THelper()

	wt.Log("waiting for CR to be ready")
	wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(
		jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
	)
}

// getDependencies extracts the typed Dependencies from the unstructured CR.
func getDependencies(wt *testf.WithT, cr *unstructured.Unstructured) ccmapi.Dependencies {
	var deps ccmapi.Dependencies

	// Extract spec.dependencies into the typed struct
	specDeps, found, err := unstructured.NestedFieldCopy(cr.Object, "spec", "dependencies")
	wt.Expect(err).NotTo(HaveOccurred(), "failed to extract spec.dependencies")
	wt.Expect(found).To(BeTrue(), "spec.dependencies should exist")

	// Convert the map to JSON and back to properly populate the typed struct
	depsBytes, err := json.Marshal(specDeps)
	wt.Expect(err).NotTo(HaveOccurred(), "failed to marshal dependencies")

	err = json.Unmarshal(depsBytes, &deps)
	wt.Expect(err).NotTo(HaveOccurred(), "failed to unmarshal dependencies")

	return deps
}

// getManagedDependencyDeployments returns all deployments managed by the cloud manager,
// identified by the InfrastructurePartOf label. It asserts that every such deployment
// is located in one of the expected namespaces, failing the test if any deployment is
// found in an unexpected namespace (e.g., misplaced resources).
// Note: Only includes operator deployments, not operand deployments (e.g., cert-manager-operator,
// not the cert-manager controller/webhook which are managed by the operator itself).
func getManagedDependencyDeployments(wt *testf.WithT, cr *unstructured.Unstructured) []unstructured.Unstructured {
	deps := getDependencies(wt, cr)

	lwsNS := deps.LWS.GetNamespace()
	sailNS := deps.SailOperator.GetNamespace()

	wt.Expect(lwsNS).NotTo(BeEmpty(), "lws namespace should be set by kubebuilder default")
	wt.Expect(sailNS).NotTo(BeEmpty(), "sailOperator namespace should be set by kubebuilder default")

	expectedNamespaces := map[string]bool{
		certManagerOperandNS:  true,
		certManagerOperatorNS: true,
		lwsNS:                 true,
		sailNS:                true,
	}

	// Get all deployments with the InfrastructurePartOf label
	allDeployments := wt.List(gvk.Deployment,
		client.MatchingLabels{labels.InfrastructurePartOf: getPartOfLabelValue()},
	).Eventually().Should(Not(BeEmpty()))

	// Assert no deployment is in an unexpected namespace
	for _, dep := range allDeployments {
		wt.Expect(expectedNamespaces).To(HaveKey(dep.GetNamespace()),
			"deployment %q is in unexpected namespace %q; expected one of %v",
			dep.GetName(), dep.GetNamespace(), expectedNamespaces,
		)
	}

	return allDeployments
}

// getManagedDependencyDeploymentByName returns the first deployment whose name contains
// the given substring from the managed dependency deployments list.
func getManagedDependencyDeploymentByName(t *testing.T, wt *testf.WithT, cr *unstructured.Unstructured, name string) unstructured.Unstructured {
	t.Helper()

	deps := getManagedDependencyDeployments(wt, cr)
	for _, dep := range deps {
		if strings.Contains(dep.GetName(), name) {
			return dep
		}
	}

	t.Fatalf("no managed dependency deployment found with name containing %q", name)
	return unstructured.Unstructured{} // unreachable
}

// getCertManagerOperandNamespace returns the hardcoded cert-manager operand namespace.
func getCertManagerOperandNamespace() string {
	return certManagerOperandNS
}

// getSailOperatorNamespace reads the sail operator namespace from the CR spec using typed methods.
func getSailOperatorNamespace(wt *testf.WithT, cr *unstructured.Unstructured) string {
	deps := getDependencies(wt, cr)
	return deps.SailOperator.GetNamespace()
}

// waitForDeploymentsAvailable waits until all managed dependency deployments
// have the Available=True condition, meaning they're actually running.
func waitForDeploymentsAvailable(wt *testf.WithT, deployments []unstructured.Unstructured) {
	for _, dep := range deployments {
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: dep.GetName(), Namespace: dep.GetNamespace(),
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

// getPartOfLabelValue returns the value used for the InfrastructurePartOf label
// by converting the provider's kind to lowercase.
func getPartOfLabelValue() string {
	return strings.ToLower(provider.GVK.Kind)
}

// getAllManagedNamespaces returns all namespaces managed by the cloud manager,
// identified by the InfrastructurePartOf label matching the provider's kind.
func getAllManagedNamespaces(wt *testf.WithT) []unstructured.Unstructured {
	return wt.List(gvk.Namespace,
		client.MatchingLabels{labels.InfrastructurePartOf: getPartOfLabelValue()},
	).Eventually().Should(Not(BeEmpty()))
}

// getCertManagerOperandDeployments returns the cert-manager operand deployments
// from the cert-manager namespace. These are created by the cert-manager operator,
// not directly by the cloud manager, so they don't have the InfrastructurePartOf label.
// Expected deployments: cert-manager, cert-manager-webhook, cert-manager-cainjector.
func getCertManagerOperandDeployments(wt *testf.WithT) []unstructured.Unstructured {
	allDeployments := wt.List(gvk.Deployment,
		client.InNamespace(certManagerOperandNS),
	).Eventually().Should(Not(BeEmpty()))

	// Filter for cert-manager deployments by name prefix
	var certManagerDeployments []unstructured.Unstructured
	for _, dep := range allDeployments {
		name := dep.GetName()
		if strings.HasPrefix(name, "cert-manager") {
			certManagerDeployments = append(certManagerDeployments, dep)
		}
	}

	return certManagerDeployments
}

// waitForCertManagerOperandAvailable waits until all cert-manager operand deployments
// are Available, verifying that cert-manager is running correctly.
func waitForCertManagerOperandAvailable(wt *testf.WithT) {
	deployments := getCertManagerOperandDeployments(wt)
	wt.Expect(deployments).To(HaveLen(3), "expected 3 cert-manager operand deployments (controller, webhook, cainjector)")
	waitForDeploymentsAvailable(wt, deployments)
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

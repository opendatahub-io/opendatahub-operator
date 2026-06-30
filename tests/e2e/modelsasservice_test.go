package e2e_test

import (
	"fmt"
	"net"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelsasservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type ModelsAsServiceTestCtx struct {
	*ComponentTestCtx
}

const (
	// Subcomponent field name in JSON (matches the field name in KserveCommonSpec).
	// This must match the JSON tag in KserveCommonSpec.ModelsAsService.
	modelsAsServiceFieldName = "modelsAsService"

	// Gateway constants from modelsasservice package.
	maasGatewayNamespace = modelsasservice.DefaultGatewayNamespace
	maasGatewayName      = modelsasservice.DefaultGatewayName

	// Gateway class for OpenShift default ingress controller.
	// Reference: https://github.com/opendatahub-io/models-as-a-service/blob/main/deployment/base/networking/maas/maas-gateway-api.yaml
	maasGatewayClassName = "openshift-default"

	// PostgreSQL constants for the MaaS database dependency.
	// Reference: https://github.com/opendatahub-io/models-as-a-service/blob/main/scripts/deploy.sh
	maasPostgresName     = "maas-postgres"
	maasPostgresImage    = "registry.redhat.io/rhel9/postgresql-15:latest"
	maasPostgresUser     = "maas"
	maasPostgresPassword = "maas-e2e-test" //nolint:gosec // test-only credential, not a real secret
	maasPostgresDB       = "maas"
	maasDBConfigSecret   = "maas-db-config" //nolint:gosec // secret name reference, not a credential
)

func modelsAsServiceTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewSubComponentTestCtx(t, &componentApi.ModelsAsService{}, componentApi.KserveKind, modelsAsServiceFieldName)
	require.NoError(t, err)

	componentCtx := ModelsAsServiceTestCtx{
		ComponentTestCtx: ct,
	}

	// Set per-operation timeout defaults for all operations in this suite.
	// MaaS startup involves multiple dependencies (postgres, gateway, maas-controller leader election,
	// ServiceAccount/TLS cert provisioning) that can take longer than the default 5m timeout.
	componentCtx.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(ct.TestTimeouts.longEventuallyTimeout),
		WithEventuallyPollingInterval(ct.TestTimeouts.defaultEventuallyPollInterval),
	}

	if componentCtx.IsXKS() {
		componentCtx.runXKSTestSuite(t)
	} else {
		componentCtx.runOpenShiftTestSuite(t)
	}
}

// commonTenantTests returns the Tenant validation test cases shared across
// OpenShift and xKS suites. Keeping them in one place ensures new Tenant
// tests are automatically exercised on both platforms.
func (tc *ModelsAsServiceTestCtx) commonTenantTests() []TestCase {
	return []TestCase{
		{"Validate Tenant CR in subscription namespace", tc.ValidateTenantInSubscriptionNamespace},
		{"Validate Tenant CRD is namespace-scoped", tc.ValidateTenantCRDNamespaceScoped},
		{"Validate Tenant singleton enforcement", tc.ValidateTenantSingletonEnforcement},
	}
}

// runOpenShiftTestSuite runs the MaaS e2e tests on OpenShift (DSC/DSCI mode).
func (tc *ModelsAsServiceTestCtx) runOpenShiftTestSuite(t *testing.T) {
	t.Helper()

	tc.createMaaSPostgres(t)
	tc.createMaaSGateway(t)
	tc.EnsureParentComponentEnabled(t)
	tc.UpdateSubComponentStateInDataScienceCluster(t, operatorv1.Managed)

	testCases := tc.commonTenantTests()
	testCases = append(testCases,
		TestCase{"Validate payload-processing egress NetworkPolicy", tc.ValidatePayloadProcessingNetworkPolicy},
		TestCase{"Validate Tenant deleted on disable", tc.ValidateTenantDeletedOnDisable},
	)

	RunTestCases(t, testCases)
}

// runXKSTestSuite runs MaaS e2e tests on vanilla Kubernetes (no DSC/DSCI).
// On xKS the ModelsAsService CR is created directly (as the Helm hook does)
// and the operator selects the xKS kustomize overlay.
func (tc *ModelsAsServiceTestCtx) runXKSTestSuite(t *testing.T) {
	t.Helper()

	tc.createMaaSPostgres(t)
	tc.createMaaSGateway(t)

	testCases := []TestCase{
		{"Validate MaaS CR creation and reconciliation", tc.ValidateXKSMaaSCRCreation},
		{"Validate MaaS controller deployment running", tc.ValidateXKSMaaSControllerRunning},
		{"Validate MaaS webhook is registered", tc.ValidateXKSMaaSWebhookRegistered},
	}
	testCases = append(testCases, tc.commonTenantTests()...)
	testCases = append(testCases,
		TestCase{"Validate MaaS CR deletion cleans up resources", tc.ValidateXKSMaaSCRDeletion},
	)

	RunTestCases(t, testCases)
}

// ValidateXKSMaaSCRCreation creates the ModelsAsService CR directly (no DSC)
// and verifies the operator reconciles it to Ready.
func (tc *ModelsAsServiceTestCtx) ValidateXKSMaaSCRCreation(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	t.Log("Creating ModelsAsService CR directly (xKS mode, no DSC)")

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(tc.CreateComponent(tc.GVK)),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
			"Ready", metav1.ConditionTrue)),
		WithCustomErrorMsg("ModelsAsService CR should reach Ready=True"),
	)
}

// ValidateXKSMaaSControllerRunning verifies the maas-controller Deployment
// is created and available in the applications namespace.
func (tc *ModelsAsServiceTestCtx) ValidateXKSMaaSControllerRunning(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	t.Logf("Checking maas-controller Deployment is running in %s", tc.AppsNamespace)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      modelsasservice.MaasControllerDeploymentName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`)),
		WithCustomErrorMsg("maas-controller Deployment should be Available in %s", tc.AppsNamespace),
	)
}

// ValidateXKSMaaSWebhookRegistered verifies the MaaS validating webhook
// configuration is registered on the cluster.
func (tc *ModelsAsServiceTestCtx) ValidateXKSMaaSWebhookRegistered(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	t.Log("Checking MaaS ValidatingWebhookConfiguration exists")

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ValidatingWebhookConfiguration, types.NamespacedName{
			Name: modelsasservice.MaasControllerWebhookConfigName,
		}),
		WithCustomErrorMsg("MaaS ValidatingWebhookConfiguration should exist"),
	)
}

// ValidateXKSMaaSCRDeletion deletes the ModelsAsService CR and verifies
// the maas-controller Deployment is cleaned up.
func (tc *ModelsAsServiceTestCtx) ValidateXKSMaaSCRDeletion(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	t.Log("Deleting ModelsAsService CR to test disable lifecycle")

	tc.DeleteResource(
		WithMinimalObject(tc.GVK, types.NamespacedName{Name: tc.GetInstanceName(tc.GVK)}),
		WithForegroundDeletion(),
		WithWaitForDeletion(true),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
	)

	t.Logf("Waiting for maas-controller Deployment to be removed from %s", tc.AppsNamespace)

	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      modelsasservice.MaasControllerDeploymentName,
			Namespace: tc.AppsNamespace,
		}),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
		WithCustomErrorMsg("maas-controller Deployment should be removed after ModelsAsService CR deletion"),
	)
}

// createMaaSPostgres creates a minimal PostgreSQL instance and the maas-db-config secret
// required by the maas-api component. This mirrors the POC-grade setup from:
// https://github.com/opendatahub-io/models-as-a-service/blob/main/scripts/deploy.sh
func (tc *ModelsAsServiceTestCtx) createMaaSPostgres(t *testing.T) {
	t.Helper()

	ns := tc.AppsNamespace
	t.Logf("Creating MaaS PostgreSQL instance in namespace %s", ns)

	// Create the postgres Deployment
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Name: maasPostgresName, Namespace: ns}),
		WithTransforms(
			testf.Transform(`.metadata.labels = {"app": "%s"}`, maasPostgresName),
			testf.Transform(`.spec.replicas = 1`),
			testf.Transform(`.spec.selector.matchLabels = {"app": "%s"}`, maasPostgresName),
			testf.Transform(`.spec.template.metadata.labels = {"app": "%s"}`, maasPostgresName),
			testf.Transform(`.spec.template.spec.containers = [
				{
					"name": "postgres",
					"image": "%s",
					"env": [
						{"name": "POSTGRESQL_USER", "value": "%s"},
						{"name": "POSTGRESQL_PASSWORD", "value": "%s"},
						{"name": "POSTGRESQL_DATABASE", "value": "%s"}
					],
					"ports": [{"containerPort": 5432}],
					"volumeMounts": [{"name": "data", "mountPath": "/var/lib/pgsql/data"}],
					"resources": {
						"requests": {"memory": "256Mi", "cpu": "100m"},
						"limits": {"memory": "512Mi", "cpu": "500m"}
					},
					"readinessProbe": {
						"exec": {"command": ["/usr/libexec/check-container"]},
						"initialDelaySeconds": 5,
						"periodSeconds": 5
					}
				}
			]`, maasPostgresImage, maasPostgresUser, maasPostgresPassword, maasPostgresDB),
			testf.Transform(`.spec.template.spec.volumes = [{"name": "data", "emptyDir": {}}]`),
		),
		WithCustomErrorMsg("Failed to create PostgreSQL Deployment"),
	)

	// Create the postgres Service
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Service, types.NamespacedName{Name: maasPostgresName, Namespace: ns}),
		WithTransforms(
			testf.Transform(`.metadata.labels = {"app": "%s"}`, maasPostgresName),
			testf.Transform(`.spec.selector = {"app": "%s"}`, maasPostgresName),
			testf.Transform(`.spec.ports = [{"port": 5432, "targetPort": 5432}]`),
		),
		WithCustomErrorMsg("Failed to create PostgreSQL Service"),
	)

	// Create the maas-db-config secret with the connection URL
	dbURL := fmt.Sprintf("postgresql://%s:%s@%s/%s?sslmode=disable",
		maasPostgresUser, maasPostgresPassword, net.JoinHostPort(maasPostgresName, "5432"), maasPostgresDB)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Secret, types.NamespacedName{Name: maasDBConfigSecret, Namespace: ns}),
		WithTransforms(
			testf.Transform(`.stringData = {"DB_CONNECTION_URL": "%s"}`, dbURL),
		),
		WithCustomErrorMsg("Failed to create maas-db-config Secret"),
	)

	// Wait for the postgres deployment to become available before proceeding,
	// since maas-api reads the database on startup. Use EnsureResourceExists
	// (which polls via Eventually) rather than EnsureDeploymentReady (point-in-time)
	// because the image pull may take time on first run.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Name: maasPostgresName, Namespace: ns}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`)),
		WithCustomErrorMsg("PostgreSQL Deployment %s/%s should be available", ns, maasPostgresName),
	)

	t.Logf("MaaS PostgreSQL instance and maas-db-config secret created in namespace %s", ns)
}

// createMaaSGateway creates the openshift-default GatewayClass and the
// maas-default-gateway Gateway resource required by ModelsAsService.
// The Gateway includes both HTTP and HTTPS listeners — maas-api requires
// a gateway-owned Service with port 443 for internal host resolution.
func (tc *ModelsAsServiceTestCtx) createMaaSGateway(t *testing.T) {
	t.Helper()
	t.Logf("Creating MaaS GatewayClass and Gateway: %s/%s", maasGatewayNamespace, maasGatewayName)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: maasGatewayNamespace}),
		WithCustomErrorMsg("Failed to create/ensure namespace %s for MaaS Gateway", maasGatewayNamespace),
	)

	// GatewayClass must exist for the Gateway API controller to reconcile the Gateway.
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.GatewayClass, types.NamespacedName{Name: maasGatewayClassName}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.controllerName = "openshift.io/gateway-controller/v1"`),
		)),
		WithCustomErrorMsg("Failed to create GatewayClass %s", maasGatewayClassName),
	)

	clusterDomain, err := cluster.GetDomain(tc.Context(), tc.Client())
	require.NoError(t, err, "Failed to get cluster domain")

	hostname := fmt.Sprintf("maas.%s", clusterDomain)
	ingressCtrl, err := cluster.FindAvailableIngressController(tc.Context(), tc.Client())
	require.NoError(t, err, "Failed to find default IngressController")
	certSecretName := cluster.GetDefaultIngressCertSecretName(ingressCtrl)
	t.Logf("Using hostname=%s, certSecret=%s", hostname, certSecretName)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      maasGatewayName,
			Namespace: maasGatewayNamespace,
		}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.metadata.labels = {
				"app.kubernetes.io/name": "maas",
				"app.kubernetes.io/instance": "%s",
				"app.kubernetes.io/component": "gateway",
				"opendatahub.io/managed": "false"
			}`, maasGatewayName),
			testf.Transform(`.metadata.annotations = {"opendatahub.io/managed": "false"}`),
			testf.Transform(`.spec.gatewayClassName = "%s"`, maasGatewayClassName),
			testf.Transform(`.spec.listeners = [
				{
					"name": "http",
					"hostname": "%s",
					"port": 80,
					"protocol": "HTTP",
					"allowedRoutes": {"namespaces": {"from": "All"}}
				},
				{
					"name": "https",
					"hostname": "%s",
					"port": 443,
					"protocol": "HTTPS",
					"allowedRoutes": {"namespaces": {"from": "All"}},
					"tls": {
						"mode": "Terminate",
						"certificateRefs": [{"name": "%s"}]
					}
				}
			]`, hostname, hostname, certSecretName),
		)),
		WithCustomErrorMsg("Failed to create MaaS Gateway %s/%s", maasGatewayNamespace, maasGatewayName),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      maasGatewayName,
			Namespace: maasGatewayNamespace,
		}),
		WithCustomErrorMsg("MaaS Gateway %s/%s should exist", maasGatewayNamespace, maasGatewayName),
	)

	t.Logf("MaaS Gateway %s/%s created with HTTP+HTTPS listeners", maasGatewayNamespace, maasGatewayName)
}

const (
	tenantName           = "default-tenant"
	tenantSubscriptionNS = modelsasservice.MaaSSubscriptionNamespace
	tenantCRDName        = "tenants.maas.opendatahub.io"
)

// ValidateTenantInSubscriptionNamespace verifies that the maas-controller self-bootstrapped
// the default-tenant Tenant CR in the models-as-a-service namespace (not the operator namespace).
func (tc *ModelsAsServiceTestCtx) ValidateTenantInSubscriptionNamespace(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	t.Logf("Checking Tenant %s/%s exists with Ready condition", tenantSubscriptionNS, tenantName)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Tenant, types.NamespacedName{
			Name:      tenantName,
			Namespace: tenantSubscriptionNS,
		}),
		WithCondition(
			And(
				jq.Match(`.metadata.namespace == "%s"`, tenantSubscriptionNS),
				jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
			),
		),
		WithCustomErrorMsg("Tenant %s/%s should exist with Ready=True", tenantSubscriptionNS, tenantName),
	)
}

// ValidateTenantCRDNamespaceScoped verifies that the Tenant CRD is registered as namespace-scoped.
func (tc *ModelsAsServiceTestCtx) ValidateTenantCRDNamespaceScoped(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	t.Logf("Checking CRD %s has scope: Namespaced", tenantCRDName)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: tenantCRDName}),
		WithCondition(
			jq.Match(`.spec.scope == "Namespaced"`),
		),
		WithCustomErrorMsg("Tenant CRD %s should have scope: Namespaced", tenantCRDName),
	)
}

// ValidateTenantSingletonEnforcement verifies the CEL validation rule rejects Tenant CRs
// with names other than "default-tenant".
func (tc *ModelsAsServiceTestCtx) ValidateTenantSingletonEnforcement(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	t.Log("Verifying CEL singleton enforcement: creating Tenant with wrong name should fail")

	u := resources.GvkToUnstructured(gvk.Tenant)
	u.SetName("not-default-tenant")
	u.SetNamespace(tenantSubscriptionNS)

	err := tc.Client().Create(tc.Context(), u)
	require.Error(t, err, "creating Tenant with non-singleton name should be rejected by CEL validation")
	require.True(t, k8serr.IsInvalid(err),
		"expected Invalid status error from CEL validation, got: %v", err)

	// Clean up in case the CEL rule regresses and the create unexpectedly succeeds.
	t.Cleanup(func() {
		_ = tc.Client().Delete(tc.Context(), u)
	})
}

// ValidatePayloadProcessingNetworkPolicy verifies that the NetworkPolicy for
// payload-processing exists in the gateway namespace with correct ingress and
// egress rules.
func (tc *ModelsAsServiceTestCtx) ValidatePayloadProcessingNetworkPolicy(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	t.Logf("Validating NetworkPolicy for payload-processing in %s", maasGatewayNamespace)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.NetworkPolicy, types.NamespacedName{
			Name:      "payload-processing",
			Namespace: maasGatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.podSelector.matchLabels.app == "payload-processing"`),
			jq.Match(`.spec.policyTypes | any(. == "Ingress")`),
			jq.Match(`.spec.policyTypes | any(. == "Egress")`),
			jq.Match(`.spec.ingress | length == 2`),
			jq.Match(`.spec.egress | length == 1`),
			jq.Match(`.spec.egress[0] == {}`),
		)),
		WithCustomErrorMsg("NetworkPolicy should exist with correct ingress and egress rules for payload-processing in %s", maasGatewayNamespace),
	)
}

// ValidateTenantDeletedOnDisable verifies that the Tenant CR is deleted when MaaS is set to
// Removed and that the maas-controller Deployment is eventually removed from the application
// namespace. Teardown is driven by the ModelsAsService component reconciler (GC of owned objects)
// and maas-controller LifecycleReconciler (CleanupFinalizer on the Deployment).
// This test is the last case in the suite; cleanup_test.go handles DSC deletion and does not
// require MaaS to be re-enabled first.
func (tc *ModelsAsServiceTestCtx) ValidateTenantDeletedOnDisable(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	t.Logf("Verifying Tenant %s/%s is present before disable", tenantSubscriptionNS, tenantName)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Tenant, types.NamespacedName{
			Name:      tenantName,
			Namespace: tenantSubscriptionNS,
		}),
		WithCustomErrorMsg("Tenant should exist before disabling MaaS"),
	)

	t.Log("Disabling MaaS subcomponent (setting to Removed)")
	tc.UpdateSubComponentStateInDataScienceCluster(t, operatorv1.Removed)

	t.Logf("Waiting for Tenant %s/%s to be deleted", tenantSubscriptionNS, tenantName)
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.Tenant, types.NamespacedName{
			Name:      tenantName,
			Namespace: tenantSubscriptionNS,
		}),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithCustomErrorMsg("Tenant should be deleted when MaaS is disabled"),
	)

	t.Logf("Waiting until maas-controller Deployment is removed from %s", tc.AppsNamespace)
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      modelsasservice.MaasControllerDeploymentName,
			Namespace: tc.AppsNamespace,
		}),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
		WithCustomErrorMsg("maas-controller Deployment should be removed when MaaS is disabled."),
	)
}

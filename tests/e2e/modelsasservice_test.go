package e2e_test

import (
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	aigateway "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
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
	// Subcomponent field name in JSON (matches the field name in AIGatewayCommonSpec).
	// This must match the JSON tag in AIGatewayCommonSpec.ModelsAsAService.
	modelsAsServiceFieldName = "modelsAsAService"

	// Gateway constants from aigateway module package.
	maasGatewayNamespace = aigateway.MaaSGatewayNamespace
	maasGatewayName      = aigateway.MaaSGatewayName

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

func (tc *ModelsAsServiceTestCtx) maasDBNamespace() string {
	switch tc.FetchPlatformRelease() {
	case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
		return "redhat-ai-gateway-infra"
	default:
		return "odh-ai-gateway-infra"
	}
}

func modelsAsServiceTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewSubComponentTestCtx(t, &componentApi.ModelsAsService{}, componentApi.AIGatewayKind, modelsAsServiceFieldName)
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

	// Setup: Create prerequisites and enable the subcomponent.
	// PostgreSQL must be created before enabling the component because
	// maas-api reads the maas-db-config secret on startup.
	//
	// Note: MaaS is a sub-component of AIGateway (PR #3723) — the in-tree
	// ModelsAsService handler is deregistered, so ModelsAsServiceReady is never
	// set on the DSC. We bypass UpdateSubComponentStateInDataScienceCluster
	// (which waits for ModelsAsServiceReady) and patch the DSC directly.
	componentCtx.createMaaSPostgres(t)
	componentCtx.createMaaSGateway(t)
	componentCtx.EnsureParentComponentEnabled(t)

	// Enable modelsAsAService directly without waiting for the defunct ModelsAsServiceReady condition.
	componentCtx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, componentCtx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.aigateway.modelsAsAService.managementState = "Managed"`)),
		WithCondition(jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`)),
	)

	// Wait for AIGatewayReady=True before running test cases.
	componentCtx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, componentCtx.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "AIGatewayReady") | .status == "True"`)),
		WithEventuallyTimeout(ct.TestTimeouts.longEventuallyTimeout),
	)

	testCases := []TestCase{
		{"Validate MaasTenantConfig CR in subscription namespace", componentCtx.ValidateTenantInSubscriptionNamespace},
		{"Validate MaasTenantConfig CRD is namespace-scoped", componentCtx.ValidateTenantCRDNamespaceScoped},
		{"Validate MaasTenantConfig singleton enforcement", componentCtx.ValidateTenantSingletonEnforcement},
		{"Validate payload-processing egress NetworkPolicy", componentCtx.ValidatePayloadProcessingNetworkPolicy},
		{"Validate MaaS deployment removed on disable", componentCtx.ValidateMaaSDeploymentRemovedOnDisable},
	}

	RunTestCases(t, testCases)
}

// createMaaSPostgres creates a minimal PostgreSQL instance and the maas-db-config secret
// required by the maas-api component. This mirrors the POC-grade setup from:
// https://github.com/opendatahub-io/models-as-a-service/blob/main/scripts/deploy.sh
func (tc *ModelsAsServiceTestCtx) createMaaSPostgres(t *testing.T) {
	t.Helper()

	ns := tc.maasDBNamespace()
	t.Logf("Creating MaaS PostgreSQL instance in namespace %s", ns)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: ns}),
		WithCustomErrorMsg("Failed to create namespace %s for MaaS database", ns),
	)

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
	tenantSubscriptionNS = aigateway.MaaSSubscriptionNamespace
	tenantCRDName        = "maastenantconfigs.maas.opendatahub.io"
)

// ValidateTenantInSubscriptionNamespace verifies that the maas-controller self-bootstrapped
// the default-tenant MaasTenantConfig CR in the models-as-a-service namespace (not the operator namespace).
func (tc *ModelsAsServiceTestCtx) ValidateTenantInSubscriptionNamespace(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	t.Logf("Checking MaasTenantConfig %s/%s exists with Ready condition", tenantSubscriptionNS, tenantName)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MaasTenantConfig, types.NamespacedName{
			Name:      tenantName,
			Namespace: tenantSubscriptionNS,
		}),
		WithCondition(
			And(
				jq.Match(`.metadata.namespace == "%s"`, tenantSubscriptionNS),
				jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
			),
		),
		WithCustomErrorMsg("MaasTenantConfig %s/%s should exist with Ready=True", tenantSubscriptionNS, tenantName),
	)
}

// ValidateTenantCRDNamespaceScoped verifies that the MaasTenantConfig CRD is registered as namespace-scoped.
func (tc *ModelsAsServiceTestCtx) ValidateTenantCRDNamespaceScoped(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	t.Logf("Checking CRD %s has scope: Namespaced", tenantCRDName)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: tenantCRDName}),
		WithCondition(
			jq.Match(`.spec.scope == "Namespaced"`),
		),
		WithCustomErrorMsg("MaasTenantConfig CRD %s should have scope: Namespaced", tenantCRDName),
	)
}

// ValidateTenantSingletonEnforcement verifies the CEL validation rule rejects MaasTenantConfig CRs
// with names other than "default-tenant".
func (tc *ModelsAsServiceTestCtx) ValidateTenantSingletonEnforcement(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	t.Log("Verifying CEL singleton enforcement: creating MaasTenantConfig with wrong name should fail")

	u := resources.GvkToUnstructured(gvk.MaasTenantConfig)
	u.SetName("not-default-tenant")
	u.SetNamespace(tenantSubscriptionNS)

	err := tc.Client().Create(tc.Context(), u)
	require.Error(t, err, "creating MaasTenantConfig with non-singleton name should be rejected by CEL validation")
	require.True(t, k8serr.IsInvalid(err),
		"expected Invalid status error from CEL validation, got: %v", err)

	// Clean up in case the CEL rule regresses and the create unexpectedly succeeds.
	t.Cleanup(func() {
		_ = tc.Client().Delete(tc.Context(), u)
	})
}

// ValidatePayloadProcessingNetworkPolicy verifies that the NetworkPolicy for
// payload-processing exists in the gateway namespace (openshift-ingress) with
// correct ingress and egress policy types.
func (tc *ModelsAsServiceTestCtx) ValidatePayloadProcessingNetworkPolicy(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	ns := maasGatewayNamespace
	t.Logf("Validating NetworkPolicy for payload-processing in %s", ns)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.NetworkPolicy, types.NamespacedName{
			Name:      "payload-processing",
			Namespace: ns,
		}),
		WithCondition(And(
			jq.Match(`.spec.policyTypes | any(. == "Ingress")`),
			jq.Match(`.spec.policyTypes | any(. == "Egress")`),
			jq.Match(`.spec.ingress | length >= 1`),
			jq.Match(`.spec.egress | length >= 1`),
		)),
		WithCustomErrorMsg("NetworkPolicy payload-processing should exist with Ingress and Egress rules in %s", ns),
	)
}

// ValidateMaaSDeploymentRemovedOnDisable verifies that the maas-controller Deployment is
// removed when modelsAsAService is set to Removed.
//
// Architecture note (PR #3723): setting modelsAsAService=Removed causes AGO to remove the
// maas-controller Deployment. The MaaS Config CR and Tenant CR are owned by the AIGateway CR
// via ownerReferences — they are only GC'd when AIGateway itself is deleted. Tenant cleanup
// on sub-component disable is therefore out of scope for this test.
func (tc *ModelsAsServiceTestCtx) ValidateMaaSDeploymentRemovedOnDisable(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	t.Log("Disabling MaaS subcomponent (modelsAsAService=Removed)")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.aigateway.modelsAsAService.managementState = "Removed"`)),
		WithCondition(jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`)),
	)

	t.Logf("Waiting until maas-controller Deployment is removed from %s", tc.AppsNamespace)
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      aigateway.MaaSControllerDeploymentName,
			Namespace: tc.AppsNamespace,
		}),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
		WithCustomErrorMsg("maas-controller Deployment should be removed when modelsAsAService is disabled"),
	)
}

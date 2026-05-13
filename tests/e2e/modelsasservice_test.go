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

	// Setup: Create prerequisites and enable the subcomponent.
	// PostgreSQL must be created before enabling the component because
	// maas-api reads the maas-db-config secret on startup.
	componentCtx.createMaaSPostgres(t)
	componentCtx.createMaaSGateway(t)
	componentCtx.EnsureParentComponentEnabled(t)
	componentCtx.UpdateSubComponentStateInDataScienceCluster(t, operatorv1.Managed)

	testCases := []TestCase{
		{"Validate Tenant CR in subscription namespace", componentCtx.ValidateTenantInSubscriptionNamespace},
		{"Validate Tenant CRD is namespace-scoped", componentCtx.ValidateTenantCRDNamespaceScoped},
		{"Validate Tenant singleton enforcement", componentCtx.ValidateTenantSingletonEnforcement},
		{"Validate Tenant deleted on disable", componentCtx.ValidateTenantDeletedOnDisable},
	}

	RunTestCases(t, testCases)
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

// ValidateTenantDeletedOnDisable verifies that the Tenant CR is deleted and the maas-controller
// Deployment deletion is triggered when MaaS is set to Removed. The LifecycleReconciler in
// maas-controller drives the full teardown sequence (Tenants → ClusterRoles → CRDs →
// ClusterRoleBindings), so a cold re-start after disable can take several minutes.
// Note: we only assert DeletionTimestamp is set on the Deployment (not full removal) because
// the cleanup finalizer release depends on the maas-controller (RHOAIENG-61660).
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

	t.Logf("Verifying maas-controller Deployment deletion was requested in %s", tc.AppsNamespace)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      "maas-controller",
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(jq.Match(`.metadata.deletionTimestamp != null`)),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithCustomErrorMsg("maas-controller Deployment should have deletionTimestamp set when MaaS is disabled."),
	)
}

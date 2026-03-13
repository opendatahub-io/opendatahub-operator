package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelsasservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
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
	maasPostgresName     = "postgres"
	maasPostgresImage    = "registry.redhat.io/rhel9/postgresql-15:latest"
	maasPostgresUser     = "maas"
	maasPostgresPassword = "maas-e2e-test"
	maasPostgresDB       = "maas"
	maasDBConfigSecret   = "maas-db-config"
)

func modelsAsServiceTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewSubComponentTestCtx(t, &componentApi.ModelsAsService{}, componentApi.KserveKind, modelsAsServiceFieldName)
	require.NoError(t, err)

	componentCtx := ModelsAsServiceTestCtx{
		ComponentTestCtx: ct,
	}

	// Setup: Create PostgreSQL and the MaaS Gateway before running tests.
	// PostgreSQL must be created before enabling the component because
	// maas-api reads the maas-db-config secret on startup.
	componentCtx.createMaaSPostgres(t)
	componentCtx.createMaaSGateway(t)

	// Note: per e2e convention, do not cleanup resources; leave state for debugging.

	testCases := []TestCase{
		{"Validate subcomponent enabled", componentCtx.ValidateSubComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate subcomponent releases", componentCtx.ValidateSubComponentReleases},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate subcomponent disabled", componentCtx.ValidateSubComponentDisabled},
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
	dbURL := fmt.Sprintf("postgresql://%s:%s@%s:5432/%s?sslmode=disable",
		maasPostgresUser, maasPostgresPassword, maasPostgresName, maasPostgresDB)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Secret, types.NamespacedName{Name: maasDBConfigSecret, Namespace: ns}),
		WithTransforms(
			testf.Transform(`.stringData = {"DB_CONNECTION_URL": "%s"}`, dbURL),
		),
		WithCustomErrorMsg("Failed to create maas-db-config Secret"),
	)

	// Wait for the postgres pod to be ready before proceeding, since maas-api
	// reads the database on startup.
	tc.EnsureDeploymentReady(types.NamespacedName{Name: maasPostgresName, Namespace: ns}, 1)

	t.Logf("MaaS PostgreSQL instance and maas-db-config secret created in namespace %s", ns)
}

// createMaaSGateway creates the maas-default-gateway Gateway resource required by ModelsAsService.
// The Gateway is based on the MaaS Gateway definition from:
// https://github.com/opendatahub-io/models-as-a-service/blob/main/deployment/base/networking/maas/maas-gateway-api.yaml
func (tc *ModelsAsServiceTestCtx) createMaaSGateway(t *testing.T) {
	t.Helper()
	t.Logf("Creating MaaS Gateway: %s/%s", maasGatewayNamespace, maasGatewayName)

	// First, ensure the namespace exists
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: maasGatewayNamespace}),
		WithCustomErrorMsg("Failed to create/ensure namespace %s for MaaS Gateway", maasGatewayNamespace),
	)

	// Get the cluster domain for the Gateway hostname
	clusterDomain, err := cluster.GetDomain(tc.Context(), tc.Client())
	require.NoError(t, err, "Failed to get cluster domain")

	hostname := fmt.Sprintf("maas.%s", clusterDomain)
	t.Logf("Using hostname for MaaS Gateway: %s", hostname)

	// Create the Gateway resource
	// Using testf.Transform to build the Gateway spec dynamically
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      maasGatewayName,
			Namespace: maasGatewayNamespace,
		}),
		WithMutateFunc(testf.TransformPipeline(
			// Set labels
			testf.Transform(`.metadata.labels = {
				"app.kubernetes.io/name": "maas",
				"app.kubernetes.io/instance": "%s",
				"app.kubernetes.io/component": "gateway",
				"opendatahub.io/managed": "false"
			}`, maasGatewayName),
			// Set annotations
			testf.Transform(`.metadata.annotations = {"opendatahub.io/managed": "false"}`),
			// Set the GatewayClass
			testf.Transform(`.spec.gatewayClassName = "%s"`, maasGatewayClassName),
			// Set the HTTP listener
			testf.Transform(`.spec.listeners = [
				{
					"name": "http",
					"hostname": "%s",
					"port": 80,
					"protocol": "HTTP",
					"allowedRoutes": {
						"namespaces": {
							"from": "All"
						}
					}
				}
			]`, hostname),
		)),
		WithCustomErrorMsg("Failed to create MaaS Gateway %s/%s", maasGatewayNamespace, maasGatewayName),
	)

	// Wait for the Gateway to exist
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      maasGatewayName,
			Namespace: maasGatewayNamespace,
		}),
		WithCustomErrorMsg("MaaS Gateway %s/%s should exist", maasGatewayNamespace, maasGatewayName),
	)

	t.Logf("MaaS Gateway %s/%s created successfully", maasGatewayNamespace, maasGatewayName)
}

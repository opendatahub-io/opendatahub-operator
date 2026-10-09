package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const databaseServiceControllerDeployment = "odh-db-operator-operator"

// skipDatabaseServiceE2E skips the whole suite until RELATED_IMAGE_ODH_DB_OPERATOR_IMAGE is
// digest-pinned . Remove this call when re-enabling; the cases below stay as the
// acceptance checklist.
func skipDatabaseServiceE2E(t *testing.T) {
	t.Helper()
	t.Skip("Skipping DatabaseService e2e: RELATED_IMAGE_ODH_DB_OPERATOR_IMAGE has no imageOverrides digest yet ")
}

// skipDatabaseServiceOperatorReady skips checks that need a reconciling module operator
// (CRD/Deployment from the Helm chart, Ready conditions, releases).
func skipDatabaseServiceOperatorReady(t *testing.T) {
	t.Helper()
	t.Skip("Skipping: RELATED_IMAGE_ODH_DB_OPERATOR_IMAGE has no imageOverrides digest yet (RHOAIENG-96276)")
}

func databaseServiceTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	// RHAII sets RHAI_DISABLE_DATABASESERVICE_MODULE=true, so the handler is not
	// registered on XKS/KinD. OpenShift-only until the module is enabled there.
	tc.SkipIfXKSCluster(t)

	skipDatabaseServiceE2E(t)

	// DatabaseService CRD is services.platform.opendatahub.io, not components.platform.
	moduleGVK := gvk.DatabaseService
	moduleCRNN := types.NamespacedName{Name: componentApi.DatabaseServiceInstanceName}
	controllerNN := types.NamespacedName{
		Namespace: tc.AppsNamespace,
		Name:      databaseServiceControllerDeployment,
	}
	platformConfigNN := types.NamespacedName{
		Name:      modules.PlatformConfigName(componentApi.DatabaseServiceComponentName),
		Namespace: tc.AppsNamespace,
	}

	testCases := []TestCase{
		{"Validate component enabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.databaseservice.managementState = "Removed"`)),
				WithCondition(jq.Match(`.spec.components.databaseservice.managementState == "Removed"`)),
			)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.databaseservice.managementState = "Managed"`)),
				WithCondition(jq.Match(`.spec.components.databaseservice.managementState == "Managed"`)),
			)

			skipDatabaseServiceOperatorReady(t)

			tc.EnsureResourceExists(WithMinimalObject(moduleGVK, moduleCRNN))
			tc.EnsureResourceExists(WithMinimalObject(gvk.Deployment, controllerNN))

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.ConfigMap, platformConfigNN),
				WithCondition(jq.Match(`.data | has("%s")`, modules.PlatformVersionKey)),
				WithCustomErrorMsg("platform should create %s with %s",
					platformConfigNN.Name, modules.PlatformVersionKey),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(jq.Match(`.status.components.databaseservice.managementState == "Managed"`)),
				WithCustomErrorMsg("DSC status.components.databaseservice.managementState should be Managed"),
			)
		}},
		{"Validate releases mirrored to DSC", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)
			skipDatabaseServiceOperatorReady(t)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.databaseservice.managementState = "Managed"`)),
				WithCondition(jq.Match(`.spec.components.databaseservice.managementState == "Managed"`)),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithCondition(jq.Match(`.status.releases | length > 0`)),
				WithCustomErrorMsg("DatabaseService module CR should have releases in status"),
			)

			module := tc.FetchResource(WithMinimalObject(moduleGVK, moduleCRNN))
			moduleReleases, found, err := unstructured.NestedSlice(module.Object, "status", "releases")
			require.NoError(t, err)
			require.True(t, found, "DatabaseService module CR should have status.releases")
			require.NotEmpty(t, moduleReleases, "DatabaseService module CR status.releases should be non-empty")

			g := NewWithT(t)
			g.Eventually(func(g Gomega) {
				dsc := tc.FetchResource(WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName))
				dscReleases, found, err := unstructured.NestedSlice(
					dsc.Object,
					"status", "components", componentApi.DatabaseServiceComponentName, "releases",
				)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue(), "DSC status.components.databaseservice.releases should exist")
				g.Expect(dscReleases).To(Equal(moduleReleases),
					"DSC status.components.databaseservice.releases should match the module CR releases")
			}).
				WithTimeout(tc.TestTimeouts.componentReadinessTimeout).
				WithPolling(tc.TestTimeouts.defaultEventuallyPollInterval).
				Should(Succeed())
		}},
		{"Validate component disabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.databaseservice.managementState = "Removed"`)),
				WithCondition(jq.Match(`.spec.components.databaseservice.managementState == "Removed"`)),
			)

			skipDatabaseServiceOperatorReady(t)

			tc.EnsureResourceGone(WithMinimalObject(moduleGVK, moduleCRNN))
			tc.EnsureResourceGone(WithMinimalObject(gvk.Deployment, controllerNN))

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(jq.Match(`.status.components.databaseservice.managementState == "Removed"`)),
				WithCustomErrorMsg("DSC status.components.databaseservice.managementState should be Removed"),
			)
		}},
	}

	RunTestCases(t, testCases)
}

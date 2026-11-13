package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	databaseserviceModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/databaseservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	databaseServiceControllerDeployment = "odh-db-operator-operator"
	databaseServiceServiceAccount       = "opendatahub-db-operator"
	databaseServiceConfigMap            = "odh-databaseservice-config"
	databaseServiceSCCName              = "odh-db-internal-postgres"
	databaseServiceControllerImageEnv   = "RELATED_IMAGE_ODH_DB_OPERATOR_IMAGE"
	databaseServiceReleaseName          = "opendatahub-db-operator"
	databaseServiceLeaderElectionRole   = "odh-db-operator-leader-election-role"
	databaseServiceLeaderElectionRB     = "odh-db-operator-leader-election-rolebinding"
	databaseServiceDefaultProvider      = "rhai-db"

	databaseServiceCRDName     = "databaseservices.services.platform.opendatahub.io"
	databaseProviderCRDName    = "databaseproviders.infrastructure.opendatahub.io"
	databaseClaimCRDName       = "databaseclaims.infrastructure.opendatahub.io"
	schemaClaimCRDName         = "schemaclaims.infrastructure.opendatahub.io"
	operandsReadyConditionType = "OperandsReady"
)

// Infrastructure claim/provider kinds live on the db-operator API group and are
// not registered in this repo's typed scheme. Lifecycle e2e addresses them as
// unstructured resources only — no claims are created here.
var (
	databaseClaimGVK = schema.GroupVersionKind{
		Group:   "infrastructure.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "DatabaseClaim",
	}
	schemaClaimGVK = schema.GroupVersionKind{
		Group:   "infrastructure.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "SchemaClaim",
	}
	databaseProviderGVK = schema.GroupVersionKind{
		Group:   "infrastructure.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "DatabaseProvider",
	}
)

func databaseServiceTestSuite(t *testing.T) { //nolint:maintidx // sequential subtests sharing one CR lifecycle are clearer inline
	t.Helper()

	tc, err := NewModuleTestCtx(t, databaseserviceModule.NewHandler())
	require.NoError(t, err)

	// RHAII sets RHAI_DISABLE_DATABASESERVICE_MODULE=true, so the handler is not
	// registered on XKS/KinD. OpenShift-only until the module is enabled there.
	tc.SkipIfXKSCluster(t)

	moduleGVK := gvk.DatabaseService
	moduleCRNN := types.NamespacedName{Name: componentApi.DatabaseServiceInstanceName}
	controllerNN := types.NamespacedName{
		Namespace: tc.AppsNamespace,
		Name:      databaseServiceControllerDeployment,
	}

	testCases := []TestCase{
		{"Validate install", func(t *testing.T) {
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

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, controllerNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
				WithCustomErrorMsg("Deployment %s should be ready in %s", controllerNN.Name, controllerNN.Namespace),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{
					Namespace: tc.AppsNamespace,
					Name:      databaseServiceServiceAccount,
				}),
				WithCustomErrorMsg("ServiceAccount %s should exist in %s",
					databaseServiceServiceAccount, tc.AppsNamespace),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
						status.ConditionTypeReady, metav1.ConditionTrue),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
						status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
						operandsReadyConditionType, metav1.ConditionTrue),
					jq.Match(`.status.releases[] | select(.name == "%s") | .version != ""`,
						common.PlatformReleaseName),
				)),
				WithCustomErrorMsg("DatabaseService %s should be Ready with OperandsReady and a platform release",
					moduleCRNN.Name),
			)

			for _, crdName := range []string{
				databaseServiceCRDName,
				databaseProviderCRDName,
				databaseClaimCRDName,
				schemaClaimCRDName,
			} {
				tc.EnsureCRDEstablished(crdName)
			}

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.SecurityContextConstraints, types.NamespacedName{
					Name: databaseServiceSCCName,
				}),
				WithCustomErrorMsg("SCC %s should exist after DatabaseService install", databaseServiceSCCName),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`,
						componentApi.DatabaseServiceKind, metav1.ConditionTrue),
					jq.Match(`.status.components.databaseservice.managementState == "Managed"`),
				)),
				WithCustomErrorMsg("DSC should report DatabaseServiceReady=True and managementState=Managed"),
			)

			tc.EnsureResourcesGone(
				WithMinimalObject(databaseClaimGVK, types.NamespacedName{}),
				WithCustomErrorMsg("no DatabaseClaims should exist during module lifecycle e2e"),
			)
			tc.EnsureResourcesGone(
				WithMinimalObject(schemaClaimGVK, types.NamespacedName{}),
				WithCustomErrorMsg("no SchemaClaims should exist during module lifecycle e2e"),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: tc.AppsNamespace}),
				WithCustomErrorMsg("applications namespace %s must still exist after install", tc.AppsNamespace),
			)
		}},
		// Phase 2: release and image propagation on the already-installed operator.
		// This does not change versions.
		{"Validate release and image propagation", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.databaseservice.managementState = "Managed"`)),
				WithCondition(jq.Match(`.spec.components.databaseservice.managementState == "Managed"`)),
			)

			configNN := types.NamespacedName{
				Name:      databaseServiceConfigMap,
				Namespace: tc.AppsNamespace,
			}
			// PlatformConfigName(databaseservice) is the same ConfigMap the chart
			// mounts; platformVersion is merged by injectPlatformConfig.
			require.Equal(t, modules.PlatformConfigName(componentApi.DatabaseServiceComponentName), configNN.Name)

			expectedImage := databaseServiceRelatedImage(t, tc)

			g := NewWithT(t)
			g.Eventually(func(g Gomega) {
				dsc := tc.FetchResource(WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName))
				g.Expect(dsc).NotTo(BeNil())
				operatorVersion, _, err := unstructured.NestedString(dsc.Object, "status", "release", "version")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(operatorVersion).NotTo(BeEmpty(), "DSC .status.release.version must be populated")

				cm := &corev1.ConfigMap{}
				g.Expect(tc.Client().Get(context.Background(), configNN, cm)).To(Succeed())
				g.Expect(cm.Data).To(HaveKey(modules.PlatformVersionKey))
				platformVersion := cm.Data[modules.PlatformVersionKey]
				g.Expect(platformVersion).NotTo(BeEmpty())
				g.Expect(platformVersion).To(Equal(operatorVersion),
					"odh-databaseservice-config platformVersion should equal the operator release")

				module := &unstructured.Unstructured{}
				module.SetGroupVersionKind(moduleGVK)
				g.Expect(tc.Client().Get(context.Background(), moduleCRNN, module)).To(Succeed())
				moduleReleases, found, err := unstructured.NestedSlice(module.Object, "status", "releases")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue(), "DatabaseService status.releases should exist")
				modulePlatform, modulePlatformFound := releaseFieldsByName(moduleReleases, common.PlatformReleaseName)
				g.Expect(modulePlatformFound).To(BeTrue(), "DatabaseService should publish the %s release", common.PlatformReleaseName)
				g.Expect(modulePlatform).To(Equal(platformVersion),
					"DatabaseService platform release should match ConfigMap platformVersion")

				dscReleases, found, err := unstructured.NestedSlice(
					dsc.Object,
					"status", "components", componentApi.DatabaseServiceComponentName, "releases",
				)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue(), "DSC status.components.databaseservice.releases should exist")
				g.Expect(dscReleases).To(Equal(moduleReleases),
					"DSC status.components.databaseservice.releases should match the module CR releases")

				deployment := &appsv1.Deployment{}
				g.Expect(tc.Client().Get(context.Background(), controllerNN, deployment)).To(Succeed())
				managerImage := ""
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == "manager" {
						managerImage = container.Image
						break
					}
				}
				g.Expect(managerImage).NotTo(BeEmpty(), "odh-db-operator-operator should have a manager container")
				g.Expect(managerImage).To(Equal(expectedImage),
					"manager image should equal RELATED_IMAGE_ODH_DB_OPERATOR_IMAGE from the platform operator")
			}).
				WithTimeout(tc.TestTimeouts.longEventuallyTimeout).
				WithPolling(tc.TestTimeouts.defaultEventuallyPollInterval).
				Should(Succeed(), "DatabaseService release and image propagation should converge")
		}},
		// Phase 3: disable on an existing install; platform must stay healthy.
		// Do not delete DSC or DSCI here — only flip databaseservice to Removed.
		{"Validate disable", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.databaseservice.managementState = "Removed"`)),
				WithCondition(jq.Match(`.spec.components.databaseservice.managementState == "Removed"`)),
			)

			tc.EnsureResourceGone(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)
			tc.EnsureResourceGone(
				WithMinimalObject(gvk.Deployment, controllerNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`,
						componentApi.DatabaseServiceKind, metav1.ConditionFalse),
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .reason == "%s"`,
						componentApi.DatabaseServiceKind, status.RemovedReason),
					jq.Match(`.status.components.databaseservice.managementState == "Removed"`),
				)),
				WithCustomErrorMsg("DSC should report DatabaseServiceReady=False/Removed after disable"),
			)

			tc.ValidateModuleDisabled(t)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(
					`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
					status.ConditionTypeModulesReady, metav1.ConditionTrue,
				)),
				WithCustomErrorMsg("DSC ModulesReady should remain True after disabling DatabaseService"),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: tc.AppsNamespace}),
				WithCustomErrorMsg("applications namespace %s must still exist after disable", tc.AppsNamespace),
			)

			// BaseHandler retains the chart DatabaseService CRD (same as Ray/Spark).
			// Operand infrastructure CRDs are owned by db-operator cleanupOperands and
			// are deleted once no claims/providers remain.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: databaseServiceCRDName}),
				WithCustomErrorMsg("DatabaseService CRD should NOT be deleted on disable — BaseHandler retains chart CRDs"),
			)
			for _, crdName := range []string{
				databaseProviderCRDName,
				databaseClaimCRDName,
				schemaClaimCRDName,
			} {
				tc.EnsureResourceGone(
					WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: crdName}),
					WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
					WithCustomErrorMsg("operand CRD %s should be deleted on disable once claims/providers are gone", crdName),
				)
			}
		}},
		// Phase 4: after disable finalizers finish, chart-owned orphans must be gone.
		// Upstream retains the Internal provider PVC and admin Secret on purpose
		// (ownerRefs stripped during DatabaseService finalizer cleanup) so data can
		// be recovered or deleted by an administrator. Do not fail this case if
		// those objects remain, and do not delete them here.
		{"Validate uninstall", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.databaseservice.managementState = "Removed"`)),
				WithCondition(jq.Match(`.spec.components.databaseservice.managementState == "Removed"`)),
			)

			tc.EnsureResourceGone(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)

			tc.EnsureResourceGone(
				WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{
					Namespace: tc.AppsNamespace,
					Name:      databaseServiceServiceAccount,
				}),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)
			tc.EnsureResourceGone(
				WithMinimalObject(gvk.Role, types.NamespacedName{
					Namespace: tc.AppsNamespace,
					Name:      databaseServiceLeaderElectionRole,
				}),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)
			tc.EnsureResourceGone(
				WithMinimalObject(gvk.RoleBinding, types.NamespacedName{
					Namespace: tc.AppsNamespace,
					Name:      databaseServiceLeaderElectionRB,
				}),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)

			// Hashed chart ClusterRole(Binding) names omit app.kubernetes.io/instance
			// (and metrics-reader-binding has neither selector label nor the module SA
			// as subject). Match the chart.clusterResourceName prefix so every rendered
			// role/binding is covered, including future metrics-* suffixes.
			ensureDatabaseServiceChartRBACGone(t, tc)
			ensureDatabaseServiceClusterRoleBindingsGone(t, tc)

			tc.EnsureResourceGone(
				WithMinimalObject(gvk.SecurityContextConstraints, types.NamespacedName{
					Name: databaseServiceSCCName,
				}),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)
			tc.EnsureResourceGone(
				WithMinimalObject(gvk.ClusterRole, types.NamespacedName{
					Name: databaseServiceSCCName,
				}),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)

			tc.EnsureResourceGone(
				WithMinimalObject(databaseProviderGVK, types.NamespacedName{
					Name: databaseServiceDefaultProvider,
				}),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)
			tc.EnsureResourceGone(
				WithMinimalObject(gvk.StatefulSet, types.NamespacedName{
					Namespace: tc.AppsNamespace,
					Name:      databaseServiceDefaultProvider,
				}),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)

			tc.EnsureResourceGone(
				WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
					Namespace: tc.AppsNamespace,
					Name:      databaseServiceConfigMap,
				}),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)

			// Chart CRD retained; operand CRDs removed by db-operator finalizer.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: databaseServiceCRDName}),
				WithCustomErrorMsg("DatabaseService CRD should NOT be deleted on uninstall — BaseHandler retains chart CRDs"),
			)

			for _, crdName := range []string{
				databaseProviderCRDName,
				databaseClaimCRDName,
				schemaClaimCRDName,
			} {
				tc.EnsureResourceGone(
					WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: crdName}),
					WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
					WithCustomErrorMsg("operand CRD %s should be deleted on uninstall once claims/providers are gone", crdName),
				)
			}

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: tc.AppsNamespace}),
				WithCustomErrorMsg("applications namespace %s must still exist after uninstall", tc.AppsNamespace),
			)
		}},
	}

	RunTestCases(t, testCases)
}

// databaseServiceRelatedImage reads RELATED_IMAGE_ODH_DB_OPERATOR_IMAGE from the
// platform operator Deployment. injectModuleEnv uses that value to override the
// module operator manager image.
func databaseServiceRelatedImage(t *testing.T, tc *ComponentTestCtx) string {
	t.Helper()

	operatorDeployment := &appsv1.Deployment{}
	tc.FetchTypedResource(
		operatorDeployment,
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      tc.getControllerDeploymentName(),
			Namespace: tc.OperatorNamespace,
		}),
		WithCustomErrorMsg("Failed to fetch platform operator Deployment %s/%s",
			tc.OperatorNamespace, tc.getControllerDeploymentName()),
	)

	for _, container := range operatorDeployment.Spec.Template.Spec.Containers {
		for _, envVar := range container.Env {
			if envVar.Name == databaseServiceControllerImageEnv && envVar.Value != "" {
				return envVar.Value
			}
		}
	}

	t.Fatalf("platform operator Deployment is missing non-empty %s", databaseServiceControllerImageEnv)
	return ""
}

// databaseServiceClusterResourcePrefix mirrors chart.clusterResourceName from the
// db-operator Helm helpers: <fullname>-<sha256(release/ns)[:12]>-. Any hashed
// ClusterRole or ClusterRoleBinding from this release starts with that prefix.
func databaseServiceClusterResourcePrefix(releaseName, namespace string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%s", releaseName, namespace)))
	identityHash := hex.EncodeToString(sum[:])[:12]
	return fmt.Sprintf("%s-%s-", releaseName, identityHash)
}

// ensureDatabaseServiceChartRBACGone asserts no hashed ClusterRole or
// ClusterRoleBinding from the db-operator chart remains after uninstall.
func ensureDatabaseServiceChartRBACGone(t *testing.T, tc *ComponentTestCtx) {
	t.Helper()

	prefix := databaseServiceClusterResourcePrefix(databaseServiceReleaseName, tc.AppsNamespace)
	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		crList := &rbacv1.ClusterRoleList{}
		g.Expect(tc.Client().List(context.Background(), crList)).To(Succeed())
		var leftoverRoles []string
		for i := range crList.Items {
			if strings.HasPrefix(crList.Items[i].Name, prefix) {
				leftoverRoles = append(leftoverRoles, crList.Items[i].Name)
			}
		}
		g.Expect(leftoverRoles).To(BeEmpty(),
			"ClusterRoles with prefix %s should be removed after DatabaseService uninstall: %v",
			prefix, leftoverRoles)

		crbList := &rbacv1.ClusterRoleBindingList{}
		g.Expect(tc.Client().List(context.Background(), crbList)).To(Succeed())
		var leftoverBindings []string
		for i := range crbList.Items {
			if strings.HasPrefix(crbList.Items[i].Name, prefix) {
				leftoverBindings = append(leftoverBindings, crbList.Items[i].Name)
			}
		}
		g.Expect(leftoverBindings).To(BeEmpty(),
			"ClusterRoleBindings with prefix %s should be removed after DatabaseService uninstall: %v",
			prefix, leftoverBindings)
	}).
		WithTimeout(tc.TestTimeouts.longEventuallyTimeout).
		WithPolling(tc.TestTimeouts.defaultEventuallyPollInterval).
		Should(Succeed())
}

// ensureDatabaseServiceClusterRoleBindingsGone asserts no ClusterRoleBinding still
// subjects the module ServiceAccount. Complements the chart-name prefix check for
// any extra binding that still grants the module SA.
func ensureDatabaseServiceClusterRoleBindingsGone(t *testing.T, tc *ComponentTestCtx) {
	t.Helper()

	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		crbList := &rbacv1.ClusterRoleBindingList{}
		g.Expect(tc.Client().List(context.Background(), crbList)).To(Succeed())

		var leftover []string
		for i := range crbList.Items {
			crb := &crbList.Items[i]
			for _, subject := range crb.Subjects {
				if subject.Kind == "ServiceAccount" &&
					subject.Name == databaseServiceServiceAccount &&
					subject.Namespace == tc.AppsNamespace {
					leftover = append(leftover, crb.Name)
					break
				}
			}
		}
		g.Expect(leftover).To(BeEmpty(),
			"ClusterRoleBindings still granting %s/%s: %v",
			tc.AppsNamespace, databaseServiceServiceAccount, leftover)
	}).
		WithTimeout(tc.TestTimeouts.longEventuallyTimeout).
		WithPolling(tc.TestTimeouts.defaultEventuallyPollInterval).
		Should(Succeed())
}

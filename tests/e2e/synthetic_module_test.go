package e2e_test

import (
	"os"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/synthetice2e"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	syntheticModuleEnableEnv     = "ODH_ENABLE_SYNTHETIC_E2E_MODULE"
	syntheticModuleDefaultImage  = "quay.io/stobin/synthetic-module:syntheticmodule-0.0.2" //TODO: Replace this with a real prow built image and floating tag
	syntheticModuleCRDName       = "syntheticmodules.synthetic.platform.opendatahub.io"
	syntheticPlatformConfigName  = "odh-synthetice2e-config"
	syntheticServiceAccountName  = "synthetic-module-operator"
	syntheticClusterRoleName     = "synthetic-module-operator"
	syntheticClusterRoleBindName = "synthetic-module-operator"
)

var syntheticModuleGVK = schema.GroupVersionKind{
	Group:   "synthetic.platform.opendatahub.io",
	Version: "v1alpha1",
	Kind:    "SyntheticModule",
}

func syntheticModuleTestSuite(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize synthetic module test context")
	if tc.IsXKS() {
		t.Skip("synthetic module lifecycle coverage is OpenShift-only")
	}
	dscCtx := &DSCTestCtx{TestContext: tc}
	RunTestCases(t, []TestCase{
		{"Ensure DSCInitialization exists", dscCtx.ValidateDSCICreation},
		{"Ensure DataScienceCluster exists", dscCtx.ValidateDSCCreation},
	})

	ctx := &DAGOrderingTestCtx{TestContext: tc}
	controllerImage := os.Getenv(synthetice2e.ControllerImage)
	if controllerImage == "" {
		controllerImage = syntheticModuleDefaultImage
		t.Logf("%s is unset; using default synthetic module image %s", synthetice2e.ControllerImage, controllerImage)
	}

	envSnapshot := ctx.snapshotOperatorEnvVars(t, syntheticModuleEnableEnv, synthetice2e.ControllerImage)
	ctx.setOperatorEnvVars(t,
		corev1.EnvVar{Name: syntheticModuleEnableEnv, Value: "true"},
		corev1.EnvVar{Name: synthetice2e.ControllerImage, Value: controllerImage},
	)
	// Cleanup callbacks run in LIFO order: release a held module finalizer first,
	// then remove platform resources, and restore the operator environment last.
	// Keeping these independent ensures one cleanup failure does not skip the rest.
	t.Cleanup(func() {
		ctx.restoreOperatorEnvVars(t, envSnapshot)
	})
	t.Cleanup(func() {
		tc.SetModuleStateInPlatformCR(t, synthetice2e.ModuleName, operatorv1.Removed)
		ensureSyntheticOperatorResourcesGone(t, tc)
	})
	t.Cleanup(func() {
		clearSyntheticFaultBestEffort(t, tc)
	})

	RunTestCases(t, []TestCase{
		{"Enable known-good synthetic module", func(t *testing.T) {
			t.Helper()
			tc.SetModuleStateInPlatformCR(t, synthetice2e.ModuleName, operatorv1.Managed)
		}},
		{"Validate operator resources, ownership, and injection", func(t *testing.T) {
			t.Helper()
			validateSyntheticOperatorResources(t, tc, controllerImage)
		}},
		{"Validate platform Deployment reconciliation", func(t *testing.T) {
			t.Helper()
			nn := types.NamespacedName{Namespace: tc.AppsNamespace, Name: synthetice2e.DeploymentName}
			originalUID := tc.FetchResource(WithMinimalObject(gvk.Deployment, nn)).GetUID()
			tc.DeleteResource(WithMinimalObject(gvk.Deployment, nn), WithWaitForDeletion(false))
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, nn),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.metadata.uid != "%s"`, originalUID),
					jq.Match(`.status.availableReplicas >= 1`),
				)),
			)
		}},
		{"Validate platform-owned field drift correction", func(t *testing.T) {
			t.Helper()
			nn := types.NamespacedName{Namespace: tc.AppsNamespace, Name: synthetice2e.DeploymentName}
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.Deployment, nn),
				WithMutateFunc(testf.Transform(`(.spec.template.spec.containers[] | select(.name == "manager") | .env[] | select(.name == "APPLICATIONS_NAMESPACE") | .value) = "tampered"`)),
			)
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, nn),
				WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
				WithCondition(jq.Match(`any(.spec.template.spec.containers[] | select(.name == "manager") | .env[]; .name == "APPLICATIONS_NAMESPACE" and .value == "%s")`, tc.AppsNamespace)),
			)
		}},
		{"Validate platform CRD reconciliation", func(t *testing.T) {
			t.Helper()
			nn := types.NamespacedName{Name: syntheticModuleCRDName}
			originalUID := tc.FetchResource(WithMinimalObject(gvk.CustomResourceDefinition, nn)).GetUID()
			tc.DeleteResource(WithMinimalObject(gvk.CustomResourceDefinition, nn), WithWaitForDeletion(false))
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.CustomResourceDefinition, nn),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.metadata.uid != "%s"`, originalUID),
					jq.Match(`.status.conditions[] | select(.type == "Established") | .status == "True"`),
				)),
			)
		}},
		{"Validate platform configuration reconciliation", func(t *testing.T) {
			t.Helper()
			nn := types.NamespacedName{Namespace: tc.AppsNamespace, Name: syntheticPlatformConfigName}
			config := tc.FetchResource(WithMinimalObject(gvk.ConfigMap, nn))
			platformVersion, found, nestedErr := unstructured.NestedString(config.Object, "data", "platformVersion")
			require.NoError(t, nestedErr)
			require.True(t, found)
			require.NotEmpty(t, platformVersion)
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.ConfigMap, nn),
				WithMutateFunc(func(obj *unstructured.Unstructured) error {
					return unstructured.SetNestedStringMap(obj.Object, map[string]string{
						"platformVersion": "tampered",
						"moduleOwned":     "preserved",
					}, "data")
				}),
			)
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.ConfigMap, nn),
				WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.data.platformVersion == "%s"`, platformVersion),
					jq.Match(`.data.moduleOwned == "preserved"`),
				)),
			)
		}},
		{"Create synthetic module CR", func(t *testing.T) {
			t.Helper()
			moduleCR := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{"message": "synthetic module ready"},
			}}
			moduleCR.SetGroupVersionKind(syntheticModuleGVK)
			moduleCR.SetName(synthetice2e.CRName)
			tc.EventuallyResourceCreatedOrUpdated(
				WithObjectToCreate(moduleCR),
				WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
			)
		}},
		{"Validate healthy platform aggregation", func(t *testing.T) {
			t.Helper()
			waitForSyntheticHealthy(t, tc)
		}},
		{"Validate Ready false aggregation and recovery", func(t *testing.T) {
			t.Helper()
			setSyntheticFault(t, tc, synthetice2e.FaultModeReadyFalse)
			waitForSyntheticPlatformNotReady(t, tc)
			clearSyntheticFault(t, tc)
			waitForSyntheticHealthy(t, tc)
		}},
		{"Validate degraded aggregation and recovery", func(t *testing.T) {
			t.Helper()
			setSyntheticFault(t, tc, synthetice2e.FaultModeDegraded)
			tc.EnsureResourceExists(
				WithMinimalObject(syntheticModuleGVK, types.NamespacedName{Name: synthetice2e.CRName}),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "Degraded") | .status == "True"`)),
			)
			waitForSyntheticPlatformNotReady(t, tc)
			clearSyntheticFault(t, tc)
			waitForSyntheticHealthy(t, tc)
		}},
		{"Validate stale generation aggregation and recovery", func(t *testing.T) {
			t.Helper()
			tc.EventuallyResourcePatched(
				WithMinimalObject(syntheticModuleGVK, types.NamespacedName{Name: synthetice2e.CRName}),
				WithMutateFunc(testf.Transform(`.spec.message = "generation bump for stale status test"`)),
				WithCondition(jq.Match(`.metadata.generation >= 2`)),
			)
			waitForSyntheticHealthy(t, tc)
			setSyntheticFault(t, tc, synthetice2e.FaultModeStaleGeneration)
			tc.EnsureResourceExists(
				WithMinimalObject(syntheticModuleGVK, types.NamespacedName{Name: synthetice2e.CRName}),
				WithCondition(jq.Match(`.status.observedGeneration != .metadata.generation`)),
			)
			waitForSyntheticPlatformNotReady(t, tc)
			clearSyntheticFault(t, tc)
			waitForSyntheticHealthy(t, tc)
		}},
		{"Validate stale platform release aggregation and recovery", func(t *testing.T) {
			t.Helper()
			setSyntheticFault(t, tc, synthetice2e.FaultModeStalePlatformRelease)
			tc.EnsureResourceExists(
				WithMinimalObject(syntheticModuleGVK, types.NamespacedName{Name: synthetice2e.CRName}),
				WithCondition(jq.Match(`.status.releases[] | select(.name == "platform") | .version == "fault-injected-platform-release"`)),
			)
			waitForSyntheticPlatformAwaitingReadiness(t, tc)
			clearSyntheticFault(t, tc)
			waitForSyntheticHealthy(t, tc)
		}},
		{"Validate two-phase cleanup", func(t *testing.T) {
			t.Helper()
			setSyntheticFault(t, tc, synthetice2e.FaultModeHoldFinalizer)
			tc.SetModuleStateInPlatformCR(t, synthetice2e.ModuleName, operatorv1.Removed)
			tc.EnsureResourceExists(
				WithMinimalObject(syntheticModuleGVK, types.NamespacedName{Name: synthetice2e.CRName}),
				WithCondition(jq.Match(`.metadata.deletionTimestamp != null`)),
			)
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace, Name: synthetice2e.DeploymentName}),
				WithCondition(jq.Match(`.status.availableReplicas >= 1`)),
			)
			clearSyntheticFault(t, tc)
			tc.EnsureResourceGone(
				WithMinimalObject(syntheticModuleGVK, types.NamespacedName{Name: synthetice2e.CRName}),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			)
			validateSyntheticOperatorResourcesRemoved(t, tc)
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "ModulesReady") | .status == "True" and .reason == "NoManagedModules"`)),
			)
		}},
	})
}

func validateSyntheticOperatorResources(t *testing.T, tc *TestContext, controllerImage string) {
	t.Helper()
	platform := tc.FetchResource(WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName))
	ownerCondition := jq.Match(
		`any(.metadata.ownerReferences[]; .controller == true and .kind == "Platform" and .name == "%s" and .uid == "%s")`,
		platform.GetName(), platform.GetUID(),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: syntheticModuleCRDName}),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Established") | .status == "True"`)),
	)

	resources := []struct {
		gvk schema.GroupVersionKind
		nn  types.NamespacedName
	}{
		{gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace, Name: synthetice2e.DeploymentName}},
		{gvk.ServiceAccount, types.NamespacedName{Namespace: tc.AppsNamespace, Name: syntheticServiceAccountName}},
		{gvk.ClusterRole, types.NamespacedName{Name: syntheticClusterRoleName}},
		{gvk.ClusterRoleBinding, types.NamespacedName{Name: syntheticClusterRoleBindName}},
		{gvk.ConfigMap, types.NamespacedName{Namespace: tc.AppsNamespace, Name: syntheticPlatformConfigName}},
	}
	for _, resource := range resources {
		tc.EnsureResourceExists(
			WithMinimalObject(resource.gvk, resource.nn),
			WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
			WithCondition(ownerCondition),
		)
	}

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace, Name: synthetice2e.DeploymentName}),
		WithCondition(And(
			jq.Match(`.status.availableReplicas >= 1`),
			jq.Match(`.spec.template.spec.containers[] | select(.name == "manager") | .image == "%s"`, controllerImage),
			jq.Match(`any(.spec.template.spec.containers[] | select(.name == "manager") | .env[]; .name == "APPLICATIONS_NAMESPACE" and .value == "%s")`, tc.AppsNamespace),
			jq.Match(`any(.spec.template.spec.containers[] | select(.name == "manager") | .env[]; .name == "MONITORING_NAMESPACE" and .value == "%s")`, tc.MonitoringNamespace),
		)),
	)
}

func validateSyntheticOperatorResourcesRemoved(t *testing.T, tc *TestContext) {
	t.Helper()
	ensureSyntheticOperatorResourcesGone(t, tc)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: syntheticModuleCRDName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Established") | .status == "True"`)),
	)
}

func ensureSyntheticOperatorResourcesGone(t *testing.T, tc *TestContext) {
	t.Helper()
	resources := []struct {
		gvk schema.GroupVersionKind
		nn  types.NamespacedName
	}{
		{gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace, Name: synthetice2e.DeploymentName}},
		{gvk.ServiceAccount, types.NamespacedName{Namespace: tc.AppsNamespace, Name: syntheticServiceAccountName}},
		{gvk.ClusterRole, types.NamespacedName{Name: syntheticClusterRoleName}},
		{gvk.ClusterRoleBinding, types.NamespacedName{Name: syntheticClusterRoleBindName}},
		{gvk.ConfigMap, types.NamespacedName{Namespace: tc.AppsNamespace, Name: syntheticPlatformConfigName}},
	}
	for _, resource := range resources {
		tc.EnsureResourceGone(
			WithMinimalObject(resource.gvk, resource.nn),
			WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		)
	}
}

func setSyntheticFault(t *testing.T, tc *TestContext, mode string) {
	t.Helper()
	tc.EventuallyResourcePatched(
		WithMinimalObject(syntheticModuleGVK, types.NamespacedName{Name: synthetice2e.CRName}),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			annotations := obj.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations[synthetice2e.FaultAnnotationKey] = mode
			obj.SetAnnotations(annotations)
			return nil
		}),
	)
}

func clearSyntheticFault(t *testing.T, tc *TestContext) {
	t.Helper()
	tc.EventuallyResourcePatched(
		WithMinimalObject(syntheticModuleGVK, types.NamespacedName{Name: synthetice2e.CRName}),
		WithMutateFunc(removeSyntheticFault),
	)
}

func clearSyntheticFaultBestEffort(t *testing.T, tc *TestContext) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Logf("best-effort synthetic fault cleanup failed: %v", recovered)
		}
	}()

	tc.EventuallyResourcePatched(
		WithMinimalObject(syntheticModuleGVK, types.NamespacedName{Name: synthetice2e.CRName}),
		WithMutateFunc(removeSyntheticFault),
		WithIgnoreNotFound(true),
		WithAcceptableErr(func(err error) bool {
			return meta.IsNoMatchError(err) || k8serr.IsNotFound(err)
		}, "SyntheticModuleAlreadyRemoved"),
	)
}

func removeSyntheticFault(obj *unstructured.Unstructured) error {
	annotations := obj.GetAnnotations()
	delete(annotations, synthetice2e.FaultAnnotationKey)
	obj.SetAnnotations(annotations)
	return nil
}

func waitForSyntheticHealthy(t *testing.T, tc *TestContext) {
	t.Helper()
	config := tc.FetchResource(WithMinimalObject(
		gvk.ConfigMap,
		types.NamespacedName{Namespace: tc.AppsNamespace, Name: syntheticPlatformConfigName},
	))
	platformVersion, found, err := unstructured.NestedString(config.Object, "data", "platformVersion")
	require.NoError(t, err)
	require.True(t, found)
	require.NotEmpty(t, platformVersion)

	tc.EnsureResourceExists(
		WithMinimalObject(syntheticModuleGVK, types.NamespacedName{Name: synthetice2e.CRName}),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(And(
			jq.Match(`.status.observedGeneration == .metadata.generation`),
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
			jq.Match(`.status.releases[] | select(.name == "platform") | .version == "%s"`, platformVersion),
		)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "ModulesReady") | .status == "True"`),
			jq.Match(`.status.conditions[] | select(.type == "ProvisioningProgress") | .status == "True"`),
		)),
	)
}

func waitForSyntheticPlatformAwaitingReadiness(t *testing.T, tc *TestContext) {
	t.Helper()
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "ProvisioningProgress") | .status == "False" and .reason == "AwaitingReadiness"`)),
	)
}

func waitForSyntheticPlatformNotReady(t *testing.T, tc *TestContext) {
	t.Helper()
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, tc.PlatformNamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "ModulesReady") | .status == "False"`)),
	)
}

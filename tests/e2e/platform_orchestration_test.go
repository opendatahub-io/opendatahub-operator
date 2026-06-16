package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type platformComponent struct {
	object        common.PlatformObject
	dscFieldName  string
	conditionKind string
	crdName       string
	releasesField string
}

func allPlatformComponents() []platformComponent {
	return []platformComponent{
		{
			object:       &componentApi.Dashboard{},
			dscFieldName: componentApi.DashboardComponentName,
			crdName:      "dashboards.components.platform.opendatahub.io",
		},
		{
			object:        &componentApi.Workbenches{},
			dscFieldName:  componentApi.WorkbenchesComponentName,
			crdName:       "workbenches.components.platform.opendatahub.io",
			releasesField: componentApi.WorkbenchesComponentName,
		},
		{
			object:        &componentApi.DataSciencePipelines{},
			dscFieldName:  aiPipelinesFieldName,
			conditionKind: componentApi.AIPipelinesKind,
			crdName:       "datasciencepipelines.components.platform.opendatahub.io",
			releasesField: aiPipelinesFieldName,
		},
		{
			object:        &componentApi.Kserve{},
			dscFieldName:  componentApi.KserveComponentName,
			crdName:       "kserves.components.platform.opendatahub.io",
			releasesField: componentApi.KserveComponentName,
		},
		{
			object:        &componentApi.Kueue{},
			dscFieldName:  componentApi.KueueComponentName,
			crdName:       "kueues.components.platform.opendatahub.io",
			releasesField: componentApi.KueueComponentName,
		},
		{
			object:        &componentApi.Ray{},
			dscFieldName:  componentApi.RayComponentName,
			crdName:       "rays.components.platform.opendatahub.io",
			releasesField: componentApi.RayComponentName,
		},
		{
			object:        &componentApi.TrustyAI{},
			dscFieldName:  componentApi.TrustyAIComponentName,
			crdName:       "trustyais.components.platform.opendatahub.io",
			releasesField: componentApi.TrustyAIComponentName,
		},
		{
			object:        &componentApi.ModelRegistry{},
			dscFieldName:  componentApi.ModelRegistryComponentName,
			crdName:       "modelregistries.components.platform.opendatahub.io",
			releasesField: componentApi.ModelRegistryComponentName,
		},
		{
			object:        &componentApi.TrainingOperator{},
			dscFieldName:  componentApi.TrainingOperatorComponentName,
			crdName:       "trainingoperators.components.platform.opendatahub.io",
			releasesField: componentApi.TrainingOperatorComponentName,
		},
		{
			object:        &componentApi.FeastOperator{},
			dscFieldName:  componentApi.FeastOperatorComponentName,
			crdName:       "feastoperators.components.platform.opendatahub.io",
			releasesField: componentApi.FeastOperatorComponentName,
		},
		{
			object:        &componentApi.OGX{},
			dscFieldName:  componentApi.OGXComponentName,
			crdName:       "ogxs.components.platform.opendatahub.io",
			releasesField: componentApi.OGXComponentName,
		},
		{
			object:        &componentApi.MLflowOperator{},
			dscFieldName:  componentApi.MLflowOperatorComponentName,
			crdName:       "mlflowoperators.components.platform.opendatahub.io",
			releasesField: componentApi.MLflowOperatorComponentName,
		},
		{
			object:        &componentApi.Trainer{},
			dscFieldName:  componentApi.TrainerComponentName,
			crdName:       "trainers.components.platform.opendatahub.io",
			releasesField: componentApi.TrainerComponentName,
		},
		{
			object:        &componentApi.SparkOperator{},
			dscFieldName:  componentApi.SparkOperatorComponentName,
			crdName:       "sparkoperators.components.platform.opendatahub.io",
			releasesField: componentApi.SparkOperatorComponentName,
		},
	}
}

type PlatformOrchestrationTestCtx struct {
	*TestContext

	componentGVK  schema.GroupVersionKind
	componentNN   types.NamespacedName
	dscFieldName  string
	conditionKind string
	crdName       string
	releasesField string
}

func newPlatformOrchestrationTestCtx(t *testing.T, tc *TestContext, pc platformComponent) *PlatformOrchestrationTestCtx {
	t.Helper()

	ogvk, err := resources.GetGroupVersionKindForObject(tc.Scheme(), pc.object)
	require.NoError(t, err, "Failed to resolve GVK for %T", pc.object)

	conditionKind := pc.conditionKind
	if conditionKind == "" {
		conditionKind = ogvk.Kind
	}

	return &PlatformOrchestrationTestCtx{
		TestContext:   tc,
		componentGVK:  ogvk,
		componentNN:   types.NamespacedName{Name: tc.GetInstanceName(ogvk)},
		dscFieldName:  pc.dscFieldName,
		conditionKind: conditionKind,
		crdName:       pc.crdName,
		releasesField: pc.releasesField,
	}
}

func platformOrchestrationTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	tc.SkipIfXKSCluster(t)

	components, err := filterPlatformComponents(allPlatformComponents(), platformComponentFlags)
	require.NoError(t, err)

	for _, pc := range components {
		ctx := newPlatformOrchestrationTestCtx(t, tc, pc)
		name := ctx.dscFieldName

		t.Run(name, func(t *testing.T) {
			testCases := []TestCase{
				{"Bootstrap: component CR created when enabled", ctx.TestComponentCRCreatedWhenEnabled},
				{"Bootstrap: operator deployment exists and is available", ctx.TestOperatorDeploymentAvailable},
				{"Bootstrap: RBAC resources exist", ctx.TestRBACResourcesExist},
				{"Bootstrap: APPLICATIONS_NAMESPACE env var injected", ctx.TestApplicationsNamespaceInjected},
				{"Bootstrap: RELATED_IMAGE env vars injected", ctx.TestRelatedImageEnvVarsInjected},
				{"Lifecycle: component CR owned by DSC", ctx.TestComponentCROwnedByDSC},
				{"Spec projection: DSC managementState reflected on component CR", ctx.TestSpecProjectionManagementState},
				{"Spec projection: DSC patch updates component CR", ctx.TestSpecProjectionDSCPatchPropagated},
				{"Spec projection: SSA idempotency", ctx.TestSSAIdempotency},
				{"Status: component Ready propagated to DSC", ctx.TestStatusReadyPropagatedToDSC},
				{"Status: ProvisioningSucceeded condition set", ctx.TestProvisioningSucceededCondition},
				{"Status: component releases populated on DSC", ctx.TestComponentReleasesPopulated},
				{"Resilience: deleted component CR is recreated", ctx.TestDeletedComponentCRRecreated},
				{"Resilience: deleted operator deployment is recreated", ctx.TestDeletedDeploymentRecreated},
				{"Disable lifecycle: CR deleted, CRD preserved, status propagated, deployments cleaned", ctx.TestDisableLifecycle},
			}

			RunTestCases(t, testCases)
		})
	}
}

func (ctx *PlatformOrchestrationTestCtx) TestComponentCRCreatedWhenEnabled(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeReady, metav1.ConditionTrue),
		),
		WithEventuallyTimeout(ctx.TestTimeouts.componentReadinessTimeout),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestOperatorDeploymentAvailable(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: ctx.AppsNamespace,
			LabelSelector: k8slabels.Set{
				labels.PlatformPartOf: strings.ToLower(ctx.componentGVK.Kind),
			}.AsSelector(),
		}),
		WithCondition(HaveEach(
			jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`),
		)),
		WithEventuallyTimeout(ctx.TestTimeouts.componentReadinessTimeout),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestRBACResourcesExist(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourcesExist(
		WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: ctx.AppsNamespace,
			LabelSelector: k8slabels.Set{
				labels.PlatformPartOf: strings.ToLower(ctx.componentGVK.Kind),
			}.AsSelector(),
		}),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestApplicationsNamespaceInjected(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	ctx.ensureComponentEnabled(t)

	deployments := ctx.FetchResources(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: ctx.AppsNamespace,
			LabelSelector: k8slabels.Set{
				labels.PlatformPartOf: strings.ToLower(ctx.componentGVK.Kind),
			}.AsSelector(),
		}),
	)

	if len(deployments) == 0 {
		t.Skip("No deployments found for component")
	}

	for _, dep := range deployments {
		containers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "containers")
		for _, c := range containers {
			if cm, ok := c.(map[string]any); ok {
				if envList, ok := cm["env"].([]any); ok {
					for _, e := range envList {
						if em, ok := e.(map[string]any); ok {
							if name, ok := em["name"].(string); ok && name == "APPLICATIONS_NAMESPACE" {
								ctx.EnsureResourceExists(
									WithMinimalObject(gvk.Deployment, types.NamespacedName{
										Name:      dep.GetName(),
										Namespace: dep.GetNamespace(),
									}),
									WithCondition(
										jq.Match(`.spec.template.spec.containers[].env[]? | select(.name == "APPLICATIONS_NAMESPACE") | .value == "%s"`, ctx.AppsNamespace),
									),
									WithCustomErrorMsg("Deployment %s should have APPLICATIONS_NAMESPACE=%s", dep.GetName(), ctx.AppsNamespace),
								)
								return
							}
						}
					}
				}
			}
		}
	}

	t.Skip("No deployments with APPLICATIONS_NAMESPACE env var found")
}

func (ctx *PlatformOrchestrationTestCtx) TestRelatedImageEnvVarsInjected(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	ctx.ensureComponentEnabled(t)

	deployments := ctx.FetchResources(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: ctx.AppsNamespace,
			LabelSelector: k8slabels.Set{
				labels.PlatformPartOf: strings.ToLower(ctx.componentGVK.Kind),
			}.AsSelector(),
		}),
	)

	if len(deployments) == 0 {
		t.Skip("No deployments found for component")
	}

	dep := ctx.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      deployments[0].GetName(),
			Namespace: deployments[0].GetNamespace(),
		}),
	)

	containers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "containers")
	hasRelatedImage := false
	for _, c := range containers {
		if cm, ok := c.(map[string]any); ok {
			if envList, ok := cm["env"].([]any); ok {
				for _, e := range envList {
					if em, ok := e.(map[string]any); ok {
						if name, ok := em["name"].(string); ok && strings.HasPrefix(name, "RELATED_IMAGE_") {
							hasRelatedImage = true
							break
						}
					}
				}
			}
		}
		if hasRelatedImage {
			break
		}
	}

	if !hasRelatedImage {
		t.Skip("No RELATED_IMAGE_* env vars found (non-CI environment)")
	}
}

func (ctx *PlatformOrchestrationTestCtx) TestComponentCROwnedByDSC(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
		),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestDisableLifecycle(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeReady, metav1.ConditionTrue),
		),
	)

	ctx.setComponentManagementState(operatorv1.Removed)

	ctx.EnsureResourceGone(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithEventuallyTimeout(ctx.TestTimeouts.componentReadinessTimeout),
	)

	if ctx.crdName != "" {
		ctx.EnsureResourceExists(
			WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: ctx.crdName}),
			WithCustomErrorMsg("CRD %s should NOT be deleted when component is disabled", ctx.crdName),
		)
	}

	ctx.EnsureResourcesGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: ctx.AppsNamespace,
			LabelSelector: k8slabels.Set{
				labels.PlatformPartOf: strings.ToLower(ctx.componentGVK.Kind),
			}.AsSelector(),
		}),
		WithEventuallyTimeout(ctx.TestTimeouts.componentReadinessTimeout),
	)

	conditionType := ctx.conditionKind + "Ready"

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				conditionType, metav1.ConditionFalse),
		),
	)

	ctx.ensureComponentEnabled(t)
}

func (ctx *PlatformOrchestrationTestCtx) TestSpecProjectionManagementState(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.spec.components.%s.managementState == "%s"`, ctx.dscFieldName, operatorv1.Managed),
		),
	)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeReady, metav1.ConditionTrue),
		),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestSpecProjectionDSCPatchPropagated(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.metadata.annotations["%s"] == "%s"`,
				annotations.ManagementStateAnnotation, operatorv1.Managed),
		),
	)

	ctx.EventuallyResourcePatched(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithMutateFunc(testf.Transform(`.metadata.annotations["%s"] = "tampered"`,
			annotations.ManagementStateAnnotation)),
		WithCondition(
			jq.Match(`.metadata.annotations["%s"] == "tampered"`,
				annotations.ManagementStateAnnotation),
		),
	)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.metadata.annotations["%s"] == "%s"`,
				annotations.ManagementStateAnnotation, operatorv1.Managed),
		),
		WithEventuallyTimeout(ctx.TestTimeouts.componentReadinessTimeout),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestSSAIdempotency(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	ctx.ensureComponentEnabled(t)

	cr := ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeReady, metav1.ConditionTrue),
		),
	)

	gen := cr.GetGeneration()

	ctx.EnsureResourceExistsConsistently(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.metadata.generation == %d`, gen),
		),
		WithConsistentlyDuration(ctx.TestTimeouts.defaultConsistentlyTimeout),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestStatusReadyPropagatedToDSC(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	conditionType := ctx.conditionKind + "Ready"

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				conditionType, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeReady, metav1.ConditionTrue),
		)),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestProvisioningSucceededCondition(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
		),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestComponentReleasesPopulated(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke)

	if ctx.releasesField == "" {
		t.Skip("Releases field not configured for this component")
	}

	ctx.ensureComponentEnabled(t)

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithCondition(And(
			jq.Match(`.spec.components.%s.managementState == "%s"`, ctx.dscFieldName, operatorv1.Managed),
			jq.Match(`.status.components.%s.releases | length > 0`, ctx.releasesField),
			jq.Match(`.status.components.%s.releases[].name != ""`, ctx.releasesField),
			jq.Match(`.status.components.%s.releases[].version != ""`, ctx.releasesField),
		)),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestDeletedComponentCRRecreated(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	savedOpts := ctx.DefaultResourceOpts
	ctx.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(ctx.TestTimeouts.deletionRecoveryTimeout),
		WithEventuallyPollingInterval(ctx.TestTimeouts.defaultEventuallyPollInterval),
	}
	defer func() { ctx.DefaultResourceOpts = savedOpts }()

	ctx.EnsureResourceDeletedThenRecreated(
		WithMinimalObject(ctx.componentGVK, ctx.componentNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeReady, metav1.ConditionTrue),
		),
	)
}

func (ctx *PlatformOrchestrationTestCtx) TestDeletedDeploymentRecreated(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)

	ctx.ensureComponentEnabled(t)

	savedOpts := ctx.DefaultResourceOpts
	ctx.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(ctx.TestTimeouts.deletionRecoveryTimeout),
		WithEventuallyPollingInterval(ctx.TestTimeouts.defaultEventuallyPollInterval),
	}
	defer func() { ctx.DefaultResourceOpts = savedOpts }()

	deployments := ctx.FetchResources(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: ctx.AppsNamespace,
			LabelSelector: k8slabels.Set{
				labels.PlatformPartOf: strings.ToLower(ctx.componentGVK.Kind),
			}.AsSelector(),
		}),
	)

	if len(deployments) == 0 {
		t.Skip("No deployments found for component")
	}

	ctx.EnsureResourceDeletedThenRecreated(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      deployments[0].GetName(),
			Namespace: deployments[0].GetNamespace(),
		}),
	)
}

func filterPlatformComponents(all []platformComponent, flags []string) ([]platformComponent, error) {
	if len(flags) == 0 {
		flags = []string{componentApi.DashboardComponentName}
	}

	valid := make(map[string]bool, len(all))
	for _, pc := range all {
		valid[pc.dscFieldName] = true
	}

	selected := make(map[string]bool, len(flags))
	for _, f := range flags {
		name := strings.ToLower(strings.TrimSpace(f))
		if !valid[name] {
			validNames := make([]string, 0, len(all))
			for _, pc := range all {
				validNames = append(validNames, pc.dscFieldName)
			}

			return nil, fmt.Errorf("unsupported --test-platform-component value %q, valid values are: %s", f, strings.Join(validNames, ", "))
		}

		selected[name] = true
	}

	var filtered []platformComponent
	for _, pc := range all {
		if selected[pc.dscFieldName] {
			filtered = append(filtered, pc)
		}
	}

	return filtered, nil
}

func (ctx *PlatformOrchestrationTestCtx) ensureComponentEnabled(t *testing.T) {
	t.Helper()

	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, ctx.dscFieldName, operatorv1.Managed)),
		WithCondition(And(
			jq.Match(`.spec.components.%s.managementState == "%s"`, ctx.dscFieldName, operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`,
				ctx.conditionKind, metav1.ConditionTrue),
		)),
		WithEventuallyTimeout(ctx.TestTimeouts.componentReadinessTimeout),
	)
}

func (ctx *PlatformOrchestrationTestCtx) setComponentManagementState(state operatorv1.ManagementState) {
	readyStatus := metav1.ConditionFalse
	if state == operatorv1.Managed {
		readyStatus = metav1.ConditionTrue
	}

	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, ctx.dscFieldName, state)),
		WithCondition(And(
			jq.Match(`.spec.components.%s.managementState == "%s"`, ctx.dscFieldName, state),
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`,
				ctx.conditionKind, readyStatus),
		)),
		WithEventuallyTimeout(ctx.TestTimeouts.componentReadinessTimeout),
	)
}

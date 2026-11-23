package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dataconnecthub"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	dchApplicationCRName  = "default-dataconnectservice"
	dchOperatorDeployment = "dc-controller-manager"
)

var dchApplicationCRGVK = schema.GroupVersionKind{
	Group:   "dataconnecthub.opendatahub.io",
	Version: "v1alpha1",
	Kind:    "DataConnectService",
}

type DataConnectHubTestCtx struct {
	*ComponentTestCtx
}

func dataConnectHubTestSuite(t *testing.T) {
	t.Helper()

	baseCtx, err := NewModuleTestCtx(t, dataconnecthub.NewHandler())
	require.NoError(t, err)
	ctx := DataConnectHubTestCtx{ComponentTestCtx: baseCtx}

	RunTestCases(t, []TestCase{{
		name:   "Validate DCH-only module ownership and cleanup",
		testFn: ctx.ValidateDCHOnlyLifecycle,
	}})
}

func (ctx *DataConnectHubTestCtx) ValidateDCHOnlyLifecycle(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)
	ctx.SkipIfXKSCluster(t)

	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.components.data.featureStore.managementState = "Removed"`),
			testf.Transform(`.spec.components.data.dataRegistry.managementState = "Removed"`),
			testf.Transform(`.spec.components.data.dataConnectHub.managementState = "Managed"`),
		)),
		WithCondition(And(
			jq.Match(`.spec.components.data.featureStore.managementState == "Removed"`),
			jq.Match(`.spec.components.data.dataRegistry.managementState == "Removed"`),
			jq.Match(`.spec.components.data.dataConnectHub.managementState == "Managed"`),
		)),
	)

	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.Platform, ctx.PlatformNamespacedName),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
		WithCondition(And(
			jq.Match(`.spec.modules.data.managementState == "Removed"`),
			jq.Match(`.spec.modules.dataconnecthub.managementState == "Managed"`),
		)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.components.data.managementState == "Managed"`)),
	)
	ctx.EnsureResourceGone(WithMinimalObject(gvk.FeastOperator, types.NamespacedName{Name: componentApi.FeastOperatorInstanceName}))

	ctx.EnsureResourceExists(
		WithMinimalObject(ctx.GVK, ctx.NamespacedName),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.spec.applicationsNamespace == "%s"`, ctx.AppsNamespace)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace, Name: dchOperatorDeployment}),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.availableReplicas >= 1`)),
	)
	ctx.EnsureResourceExists(
		WithMinimalObject(dchApplicationCRGVK, types.NamespacedName{Namespace: ctx.AppsNamespace, Name: dchApplicationCRName}),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(
			`any(.metadata.ownerReferences[]; .kind == "%s" and .name == "%s")`,
			componentApi.DataConnectHubKind,
			componentApi.DataConnectHubInstanceName,
		)),
	)

	// The child application controller must finish before the parent removes the
	// independent DCH module operator.
	ctx.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, ctx.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.data.dataConnectHub.managementState = "Removed"`)),
		WithCondition(jq.Match(`.spec.components.data.dataConnectHub.managementState == "Removed"`)),
	)
	ctx.EnsureResourceGone(
		WithMinimalObject(ctx.GVK, ctx.NamespacedName),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
	)
	ctx.EnsureResourceGone(
		WithMinimalObject(dchApplicationCRGVK, types.NamespacedName{Namespace: ctx.AppsNamespace, Name: dchApplicationCRName}),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
	)
	ctx.EnsureResourceGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: ctx.AppsNamespace, Name: dchOperatorDeployment}),
		WithEventuallyTimeout(ctx.TestTimeouts.longEventuallyTimeout),
	)
}

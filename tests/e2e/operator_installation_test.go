package e2e_test

import (
	"testing"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

func TestEnsureOperatorHealthyViaSubscriptionCorrectsOperatorGroupScope(t *testing.T) {
	const (
		namespace   = "kueue-operator"
		packageName = "kueue-operator"
		csvName     = "kueue-operator.v1.0.0"
	)
	nn := types.NamespacedName{Name: packageName, Namespace: namespace}

	operatorGroup := &operatorsv1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-operator-group", Namespace: namespace},
		Spec:       operatorsv1.OperatorGroupSpec{TargetNamespaces: []string{namespace}},
	}
	subscription := &ofapi.Subscription{
		ObjectMeta: metav1.ObjectMeta{Name: packageName, Namespace: namespace},
		Status:     ofapi.SubscriptionStatus{CurrentCSV: csvName},
	}
	csv := &ofapi.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{Name: csvName, Namespace: namespace},
		Status:     ofapi.ClusterServiceVersionStatus{Phase: ofapi.CSVPhaseSucceeded},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(operatorGroup, subscription, csv))
	require.NoError(t, err)
	baseContext, err := testf.NewTestContext(testf.WithClient(cli))
	require.NoError(t, err)
	tc := &TestContext{TestContext: baseContext, g: baseContext.NewWithT(t), logger: t}

	tc.ensureOperatorHealthyViaSubscription(nn, "stable", nil)

	namespaceObject := &corev1.Namespace{}
	require.NoError(t, cli.Get(t.Context(), types.NamespacedName{Name: namespace}, namespaceObject), "create namespace before OperatorGroup")

	updated := &operatorsv1.OperatorGroup{}
	require.NoError(t, cli.Get(t.Context(), types.NamespacedName{Name: operatorGroup.Name, Namespace: namespace}, updated))
	require.Empty(t, updated.Spec.TargetNamespaces, "nil target namespaces is an AllNamespaces OperatorGroup")

	operatorGroups := &operatorsv1.OperatorGroupList{}
	require.NoError(t, cli.List(t.Context(), operatorGroups, client.InNamespace(namespace)))
	require.Len(t, operatorGroups.Items, 1, "correct the existing group instead of creating a second group")
}

func TestEnsureOperatorGroupExistsPreservesExistingScopeUnlessCorrectionIsRequested(t *testing.T) {
	const namespace = "kueue-operator"
	nn := types.NamespacedName{Name: "kueue-operator", Namespace: namespace}
	const existingTargetNamespace = "existing-namespace"
	const requestedTargetNamespace = "requested-namespace"

	operatorGroup := &operatorsv1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-operator-group", Namespace: namespace},
		Spec:       operatorsv1.OperatorGroupSpec{TargetNamespaces: []string{existingTargetNamespace}},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(operatorGroup))
	require.NoError(t, err)
	baseContext, err := testf.NewTestContext(testf.WithClient(cli))
	require.NoError(t, err)
	tc := &TestContext{TestContext: baseContext, g: baseContext.NewWithT(t), logger: t}

	tc.ensureOperatorGroupExists(nn, []string{requestedTargetNamespace}, false)

	updated := &operatorsv1.OperatorGroup{}
	require.NoError(t, cli.Get(t.Context(), types.NamespacedName{Name: operatorGroup.Name, Namespace: namespace}, updated))
	require.Equal(t, []string{existingTargetNamespace}, updated.Spec.TargetNamespaces)
}

func TestShouldUseOLMv1(t *testing.T) {
	tests := []struct {
		name            string
		op              Operator
		forceOLMVersion string
		want            bool
	}{
		{
			name:            "capability does not select OLMv1 by default",
			op:              Operator{useOLMv1: true},
			forceOLMVersion: "default",
			want:            false,
		},
		{
			name: "selects OLMv1 by default when configured",
			op: Operator{
				useOLMv1:          true,
				useOLMv1ByDefault: true,
			},
			forceOLMVersion: "default",
			want:            true,
		},
		{
			name: "forced OLMv0 overrides the default",
			op: Operator{
				useOLMv1:          true,
				useOLMv1ByDefault: true,
			},
			forceOLMVersion: "v0",
			want:            false,
		},
		{
			name:            "forced OLMv1 uses capability",
			op:              Operator{useOLMv1: true},
			forceOLMVersion: "v1",
			want:            true,
		},
		{
			name: "forced OLMv1 rejects unsupported operators",
			op: Operator{
				useOLMv1ByDefault: true,
			},
			forceOLMVersion: "v1",
			want:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldUseOLMv1(tt.op, tt.forceOLMVersion))
		})
	}
}

func TestPlanOperatorInstall(t *testing.T) {
	localNN := types.NamespacedName{Name: "job-set", Namespace: "openshift-jobset-operator"}
	globalNN := types.NamespacedName{Name: "tempo-product", Namespace: "openshift-tempo-operator"}
	kueueNN := types.NamespacedName{Name: "kueue-operator", Namespace: "openshift-kueue-operator"}

	tests := []struct {
		name            string
		op              Operator
		forceOLMVersion string
		want            operatorInstallPlan
	}{
		{
			name:            "forced v0 requires a subscription for an unflagged local operator",
			op:              Operator{nn: localNN},
			forceOLMVersion: "v0",
			want: operatorInstallPlan{
				mode:             operatorInstallSubscription,
				targetNamespaces: []string{localNN.Namespace},
			},
		},
		{
			name: "forced v0 requires a subscription for an OLMv1 operator without widening its scope",
			op: Operator{
				nn:                kueueNN,
				useOLMv1:          true,
				useOLMv1ByDefault: true,
			},
			forceOLMVersion: "v0",
			want: operatorInstallPlan{
				mode:             operatorInstallSubscription,
				targetNamespaces: []string{kueueNN.Namespace},
			},
		},
		{
			name: "forced v0 keeps a global operator group",
			op: Operator{
				nn:                  globalNN,
				globalOperatorGroup: true,
			},
			forceOLMVersion: "v0",
			want: operatorInstallPlan{
				mode: operatorInstallSubscription,
			},
		},
		{
			name: "forced v0 keeps a skipped operator group on AllNamespaces",
			op: Operator{
				nn:                localNN,
				skipOperatorGroup: true,
			},
			forceOLMVersion: "v0",
			want: operatorInstallPlan{
				mode: operatorInstallSubscription,
			},
		},
		{
			name:            "default local operator keeps the local subscription install",
			op:              Operator{nn: localNN},
			forceOLMVersion: "default",
			want: operatorInstallPlan{
				mode:             operatorInstallLocalOperatorGroup,
				targetNamespaces: []string{localNN.Namespace},
			},
		},
		{
			name: "default global operator keeps the global subscription install",
			op: Operator{
				nn:                  globalNN,
				globalOperatorGroup: true,
			},
			forceOLMVersion: "default",
			want: operatorInstallPlan{
				mode: operatorInstallGlobalOperatorGroup,
			},
		},
		{
			name: "default skipped operator group stays on the channel install",
			op: Operator{
				nn:                localNN,
				skipOperatorGroup: true,
			},
			forceOLMVersion: "default",
			want: operatorInstallPlan{
				mode: operatorInstallChannel,
			},
		},
		{
			name: "default OLMv1 operator uses a cluster extension",
			op: Operator{
				nn:                kueueNN,
				useOLMv1:          true,
				useOLMv1ByDefault: true,
			},
			forceOLMVersion: "default",
			want:            operatorInstallPlan{mode: operatorInstallClusterExtension},
		},
		{
			name: "forced v1 uses a cluster extension only when the operator supports it",
			op: Operator{
				nn:                  globalNN,
				globalOperatorGroup: true,
				useOLMv1:            true,
			},
			forceOLMVersion: "v1",
			want:            operatorInstallPlan{mode: operatorInstallClusterExtension},
		},
		{
			name: "forced v1 without capability stays on the configured OLMv0 install",
			op: Operator{
				nn:                  globalNN,
				globalOperatorGroup: true,
				useOLMv1ByDefault:   true,
			},
			forceOLMVersion: "v1",
			want: operatorInstallPlan{
				mode: operatorInstallGlobalOperatorGroup,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, planOperatorInstall(tt.op, tt.forceOLMVersion))
		})
	}
}

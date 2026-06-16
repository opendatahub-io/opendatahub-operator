//nolint:testpackage
package precondition

import (
	"context"
	"testing"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rs/xid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestMonitorSubscriptions_CheckResult(t *testing.T) {
	tests := []struct {
		name string
		// OLM subscription names already installed in the cluster
		installedSubscriptions []string
		// subscriptions the precondition is configured to require
		requiredSubscriptions []SubscriptionDependency
		pass                  bool
		exactMessage          string
		containsMsg           []string
		notContainsMsg        []string
	}{
		{
			name:                   "all required subscriptions are installed",
			installedSubscriptions: []string{"operator-a", "operator-b"},
			requiredSubscriptions: []SubscriptionDependency{
				{Name: "operator-a", DisplayName: "Operator A"},
				{Name: "operator-b", DisplayName: "Operator B"},
			},
			pass: true,
		},
		{
			name:                   "one required subscription is missing",
			installedSubscriptions: []string{"operator-a"},
			requiredSubscriptions: []SubscriptionDependency{
				{Name: "operator-a", DisplayName: "Operator A"},
				{Name: "operator-missing", DisplayName: "Missing Operator"},
			},
			pass:           false,
			containsMsg:    []string{"Missing Operator", "subscription not found"},
			notContainsMsg: []string{"Operator A"},
		},
		{
			name: "all required subscriptions are missing",
			requiredSubscriptions: []SubscriptionDependency{
				{Name: "operator-a", DisplayName: "Operator A"},
				{Name: "operator-b", DisplayName: "Operator B"},
			},
			pass:         false,
			exactMessage: "Operator A: subscription not found; Operator B: subscription not found",
		},
		{
			name: "missing subscription with custom Message overrides subscription not found",
			requiredSubscriptions: []SubscriptionDependency{
				{Name: "operator-a", DisplayName: "Operator A", Message: "ABCDE"},
			},
			pass:           false,
			exactMessage:   "ABCDE",
			notContainsMsg: []string{"subscription not found"},
		},
		{
			name: "custom Message and default Message are joined",
			requiredSubscriptions: []SubscriptionDependency{
				{Name: "operator-a", DisplayName: "Operator A", Message: "OPQRST"},
				{Name: "operator-b", DisplayName: "Operator B"},
			},
			pass:         false,
			exactMessage: "OPQRST; Operator B: subscription not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			var objs []client.Object
			for _, name := range tt.installedSubscriptions {
				objs = append(objs, &v1alpha1.Subscription{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "openshift-operators"},
				})
			}

			cli, err := fakeclient.New(fakeclient.WithObjects(objs...))
			g.Expect(err).NotTo(HaveOccurred())

			rr := &types.ReconciliationRequest{
				Client:   cli,
				Instance: &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}},
			}

			pc := MonitorSubscriptions(tt.requiredSubscriptions)
			result, err := pc.check(t.Context(), rr)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Pass).To(Equal(tt.pass))

			if tt.exactMessage != "" {
				g.Expect(result.Message).To(Equal(tt.exactMessage))
			}
			for _, s := range tt.containsMsg {
				g.Expect(result.Message).To(ContainSubstring(s))
			}
			for _, s := range tt.notContainsMsg {
				g.Expect(result.Message).NotTo(ContainSubstring(s))
			}
		})
	}
}

func TestMonitorSubscriptions_InvalidInputPanics(t *testing.T) {
	g := NewWithT(t)

	t.Run("empty Name", func(t *testing.T) {
		g.Expect(func() {
			MonitorSubscriptions([]SubscriptionDependency{{Name: "", DisplayName: "Operator A"}})
		}).To(PanicWith(ContainSubstring("empty Name or DisplayName")))
	})

	t.Run("empty DisplayName", func(t *testing.T) {
		g.Expect(func() {
			MonitorSubscriptions([]SubscriptionDependency{{Name: "operator-a", DisplayName: ""}})
		}).To(PanicWith(ContainSubstring("empty Name or DisplayName")))
	})

	t.Run("both empty", func(t *testing.T) {
		g.Expect(func() {
			MonitorSubscriptions([]SubscriptionDependency{{}})
		}).To(PanicWith(ContainSubstring("empty Name or DisplayName")))
	})

	t.Run("nil list", func(t *testing.T) {
		g.Expect(func() {
			MonitorSubscriptions(nil)
		}).To(PanicWith(ContainSubstring("empty subscription list")))
	})

	t.Run("empty list", func(t *testing.T) {
		g.Expect(func() {
			MonitorSubscriptions([]SubscriptionDependency{})
		}).To(PanicWith(ContainSubstring("empty subscription list")))
	})
}

func TestMonitorSubscriptions_ListError(t *testing.T) {
	g := NewWithT(t)

	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return errTest
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client:   cli,
		Instance: &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}},
	}

	pc := MonitorSubscriptions([]SubscriptionDependency{
		{Name: "operator-a", DisplayName: "Operator A"},
	})

	_, err = pc.check(t.Context(), rr)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("Operator A"))
}

func TestMonitorSubscriptions_ErrorWritesConditionUnknown(t *testing.T) {
	g := NewWithT(t)

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	condType := "TestSubError"
	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	condManager := cond.NewManager(instance, status.ConditionTypeReady, condType)

	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return errTest
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	pcs := []PreCondition{
		MonitorSubscriptions(
			[]SubscriptionDependency{
				{Name: "operator-a", DisplayName: "Operator A"},
			},
			WithConditionType(condType),
			WithSeverity(common.ConditionSeverityInfo),
		),
	}

	shouldStop := RunAll(t.Context(), rr, pcs)
	g.Expect(shouldStop).To(BeFalse())

	got := condManager.GetCondition(condType)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(got.Reason).To(Equal(PreConditionFailedReason))
	g.Expect(got.Message).To(ContainSubstring("Operator A"))
}

func TestMonitorSubscriptions_CallerOverridesDefaultClusterType(t *testing.T) {
	g := NewWithT(t)

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	condType := "TestOverride"
	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	condManager := cond.NewManager(instance, status.ConditionTypeReady, condType)

	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	pcs := []PreCondition{
		MonitorSubscriptions(
			[]SubscriptionDependency{
				{Name: "operator-a", DisplayName: "Operator A"},
			},
			WithConditionType(condType),
			WithClusterTypes(cluster.ClusterTypeKubernetes),
		),
	}

	RunAll(t.Context(), rr, pcs)

	got := condManager.GetCondition(condType)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).NotTo(Equal(metav1.ConditionFalse),
		"caller passed WithClusterTypes(Kubernetes) but cluster is OpenShift — precondition should have been skipped")
}

func TestMonitorSubscriptions_IntegrationWithRunAll(t *testing.T) {
	g := NewWithT(t)

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	condType := "TestSubDeps"
	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	condManager := cond.NewManager(instance, status.ConditionTypeReady, condType)

	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	pcs := []PreCondition{
		MonitorSubscriptions(
			[]SubscriptionDependency{
				{Name: "operator-missing", DisplayName: "Missing Operator"},
			},
			WithConditionType(condType),
			WithSeverity(common.ConditionSeverityInfo),
		),
	}

	shouldStop := RunAll(t.Context(), rr, pcs)
	g.Expect(shouldStop).To(BeFalse())

	got := condManager.GetCondition(condType)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got.Reason).To(Equal(PreConditionFailedReason))
	g.Expect(got.Severity).To(Equal(common.ConditionSeverityInfo))
	g.Expect(got.Message).To(ContainSubstring("Missing Operator"))
	g.Expect(got.Message).To(ContainSubstring("subscription not found"))
}

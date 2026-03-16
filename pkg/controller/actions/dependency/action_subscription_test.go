package dependency_test

import (
	"context"
	"errors"
	"testing"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rs/xid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

var errTest = errors.New("test error")

const (
	testHappyCondition    = "Ready"
	testConditionType     = "TestSubscriptionDeps"
	testConditionTypeWide = "TestSubscriptionDepsWide"
)

func TestSubscriptionAction_AllPresent(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	sub1 := &v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operator-a",
			Namespace: "openshift-operators",
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(sub1))
	g.Expect(err).NotTo(HaveOccurred())

	instance := &componentApi.Kserve{
		ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()},
	}

	condManager := cond.NewManager(instance, testHappyCondition, testConditionType)
	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	action := dependency.NewSubscriptionAction(
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: testConditionType,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: "operator-a", DisplayName: "Operator A"},
			},
			Reason:   "SubscriptionNotFound",
			Message:  "Warning: %s not installed",
			Severity: common.ConditionSeverityInfo,
		}),
	)

	err = action(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())

	got := condManager.GetCondition(testConditionType)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionTrue))
}

func TestSubscriptionAction_OneMissing(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())

	instance := &componentApi.Kserve{
		ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()},
	}

	condManager := cond.NewManager(instance, testHappyCondition, testConditionType)
	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	action := dependency.NewSubscriptionAction(
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: testConditionType,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: "operator-missing", DisplayName: "Missing Operator"},
			},
			Reason:   "SubscriptionNotFound",
			Message:  "Warning: %s is not installed",
			Severity: common.ConditionSeverityInfo,
		}),
	)

	err = action(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())

	got := condManager.GetCondition(testConditionType)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got.Reason).To(Equal("SubscriptionNotFound"))
	g.Expect(got.Message).To(ContainSubstring("Missing Operator"))
	g.Expect(got.Severity).To(Equal(common.ConditionSeverityInfo))
}

func TestSubscriptionAction_MultipleGroups(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	sub1 := &v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operator-a",
			Namespace: "openshift-operators",
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(sub1))
	g.Expect(err).NotTo(HaveOccurred())

	instance := &componentApi.Kserve{
		ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()},
	}

	condManager := cond.NewManager(instance, testHappyCondition, testConditionType, testConditionTypeWide)
	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	action := dependency.NewSubscriptionAction(
		// Group 1: only requires operator-a (present)
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: testConditionType,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: "operator-a", DisplayName: "Operator A"},
			},
			Reason:   "SubscriptionNotFound",
			Message:  "Warning: %s not installed",
			Severity: common.ConditionSeverityInfo,
		}),
		// Group 2: requires both operator-a and operator-b (operator-b missing)
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: testConditionTypeWide,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: "operator-a", DisplayName: "Operator A"},
				{Name: "operator-b", DisplayName: "Operator B"},
			},
			Reason:   "SubscriptionNotFound",
			Message:  "Warning: %s not installed, feature X cannot be used",
			Severity: common.ConditionSeverityInfo,
		}),
	)

	err = action(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())

	// Group 1 should be satisfied
	got1 := condManager.GetCondition(testConditionType)
	g.Expect(got1).NotTo(BeNil())
	g.Expect(got1.Status).To(Equal(metav1.ConditionTrue))

	// Group 2 should be missing operator-b
	got2 := condManager.GetCondition(testConditionTypeWide)
	g.Expect(got2).NotTo(BeNil())
	g.Expect(got2.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got2.Message).To(ContainSubstring("Operator B"))
	g.Expect(got2.Message).NotTo(ContainSubstring("Operator A"))
}

func TestSubscriptionAction_MultipleMissing(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())

	instance := &componentApi.Kserve{
		ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()},
	}

	condManager := cond.NewManager(instance, testHappyCondition, testConditionType)
	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	action := dependency.NewSubscriptionAction(
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: testConditionType,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: "operator-x", DisplayName: "Operator X"},
				{Name: "operator-y", DisplayName: "Operator Y"},
			},
			Reason:   "SubscriptionNotFound",
			Message:  "Warning: %s not installed",
			Severity: common.ConditionSeverityInfo,
		}),
	)

	err = action(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())

	got := condManager.GetCondition(testConditionType)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got.Message).To(ContainSubstring("Operator X"))
	g.Expect(got.Message).To(ContainSubstring("Operator Y"))
	g.Expect(got.Message).To(ContainSubstring(" and "))
}

func TestSubscriptionAction_ListError(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return errTest
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())

	instance := &componentApi.Kserve{
		ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()},
	}

	condManager := cond.NewManager(instance, testHappyCondition, testConditionType)
	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	action := dependency.NewSubscriptionAction(
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: testConditionType,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: "operator-a", DisplayName: "Operator A"},
			},
			Reason:   "SubscriptionNotFound",
			Message:  "Warning: %s not installed",
			Severity: common.ConditionSeverityInfo,
		}),
	)

	err = action(ctx, rr)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to check Operator A subscription"))
}

func TestSubscriptionAction_ClusterTypeMatches(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())

	instance := &componentApi.Kserve{
		ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()},
	}

	condManager := cond.NewManager(instance, testHappyCondition, testConditionType)
	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	// Group restricted to "OpenShift", cluster type is "OpenShift" → check runs, subscription missing
	action := dependency.NewSubscriptionAction(
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: testConditionType,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: "operator-missing", DisplayName: "Missing Operator"},
			},
			ClusterTypes: []string{cluster.ClusterTypeOpenShift},
			Reason:       "SubscriptionNotFound",
			Message:      "Warning: %s not installed",
			Severity:     common.ConditionSeverityInfo,
		}),
	)

	err = action(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())

	got := condManager.GetCondition(testConditionType)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got.Message).To(ContainSubstring("Missing Operator"))
}

func TestSubscriptionAction_ClusterTypeNoMatch(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())

	instance := &componentApi.Kserve{
		ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()},
	}

	condManager := cond.NewManager(instance, testHappyCondition, testConditionType)
	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	// Group restricted to "OpenShift", cluster type is "Kubernetes" → check skipped, condition not set
	action := dependency.NewSubscriptionAction(
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: testConditionType,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: "operator-missing", DisplayName: "Missing Operator"},
			},
			ClusterTypes: []string{cluster.ClusterTypeOpenShift},
			Reason:       "SubscriptionNotFound",
			Message:      "Warning: %s not installed",
			Severity:     common.ConditionSeverityInfo,
		}),
	)

	err = action(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())

	// Condition remains in its initial Unknown state (not explicitly set by the action)
	got := condManager.GetCondition(testConditionType)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
}

func TestSubscriptionAction_ClusterTypeEmpty(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())

	instance := &componentApi.Kserve{
		ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()},
	}

	condManager := cond.NewManager(instance, testHappyCondition, testConditionType)
	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	// No ClusterTypes restriction → check always runs regardless of cluster type
	action := dependency.NewSubscriptionAction(
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: testConditionType,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: "operator-missing", DisplayName: "Missing Operator"},
			},
			Reason:   "SubscriptionNotFound",
			Message:  "Warning: %s not installed",
			Severity: common.ConditionSeverityInfo,
		}),
	)

	err = action(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())

	got := condManager.GetCondition(testConditionType)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got.Message).To(ContainSubstring("Missing Operator"))
}

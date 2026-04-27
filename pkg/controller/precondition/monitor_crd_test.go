package precondition_test

import (
	"context"
	"testing"

	"github.com/rs/xid"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

var testCRDGVK = schema.GroupVersionKind{
	Group:   "testprecondition.opendatahub.io",
	Version: "v1",
	Kind:    "TestPreConditionResource",
}

func TestMonitorCRD_Present(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	cli := envTest.Client()

	crd, err := envTest.RegisterCRD(ctx, testCRDGVK, "testpreconditionresources", "testpreconditionresource", apiextensionsv1.ClusterScoped)
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)

	rr := &types.ReconciliationRequest{Client: cli}

	t.Run("MonitorCRD", func(t *testing.T) {
		g := NewWithT(t)
		result, err := precondition.MonitorCRD(testCRDGVK).Check(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.Pass).To(BeTrue())
	})

	t.Run("MonitorCRDs", func(t *testing.T) {
		g := NewWithT(t)
		result, err := precondition.MonitorCRDs(testCRDGVK).Check(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.Pass).To(BeTrue())
	})
}

func TestMonitorCRD_Absent(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	absentGVK := schema.GroupVersionKind{
		Group:   "absent.opendatahub.io",
		Version: "v1",
		Kind:    "AbsentResource",
	}

	pc := precondition.MonitorCRD(absentGVK)
	result, err := pc.Check(ctx, &types.ReconciliationRequest{Client: envTest.Client()})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Pass).To(BeFalse())
	g.Expect(result.Message).To(ContainSubstring("AbsentResource"))
	g.Expect(result.Message).To(ContainSubstring("CRD not found"))
}

func TestMonitorCRDs_SomeAbsent(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	cli := envTest.Client()

	crd, err := envTest.RegisterCRD(ctx, testCRDGVK, "testpreconditionresources", "testpreconditionresource", apiextensionsv1.ClusterScoped)
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)

	absentGVK := schema.GroupVersionKind{
		Group:   "absent.opendatahub.io",
		Version: "v1",
		Kind:    "AbsentResource",
	}

	pc := precondition.MonitorCRDs(testCRDGVK, absentGVK)
	result, err := pc.Check(ctx, &types.ReconciliationRequest{Client: cli})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Pass).To(BeFalse())
	g.Expect(result.Message).To(ContainSubstring("AbsentResource"))
	g.Expect(result.Message).NotTo(ContainSubstring("TestPreConditionResource"))
}

func TestMonitorCRD_IntegrationWithRunAll(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	cli := envTest.Client()

	crd, err := envTest.RegisterCRD(ctx, testCRDGVK, "testpreconditionresources", "testpreconditionresource", apiextensionsv1.ClusterScoped)
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)

	absentGVK := schema.GroupVersionKind{
		Group:   "absent.opendatahub.io",
		Version: "v1",
		Kind:    "AbsentResource",
	}

	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
	rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

	pcs := []precondition.PreCondition{
		precondition.MonitorCRD(testCRDGVK),
		precondition.MonitorCRD(absentGVK),
	}

	shouldStop := precondition.RunAll(ctx, rr, pcs)
	g.Expect(shouldStop).To(BeFalse())

	got := condManager.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got.Message).To(ContainSubstring("AbsentResource"))
	g.Expect(got.Message).NotTo(ContainSubstring("TestPreConditionResource"))
}

//nolint:testpackage
package precondition

import (
	"context"
	"testing"

	"github.com/rs/xid"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

var testCRDGVK = schema.GroupVersionKind{
	Group:   "testprecondition.opendatahub.io",
	Version: "v1",
	Kind:    "TestPreConditionResource",
}

const testCRDName = "testpreconditionresources.testprecondition.opendatahub.io"

var testCRDGVK2 = schema.GroupVersionKind{
	Group:   "testprecondition.opendatahub.io",
	Version: "v1",
	Kind:    "TestPreConditionResource2",
}

const testCRDName2 = "testpreconditionresource2s.testprecondition.opendatahub.io"

const absentCRDName = "absentresources.absent.opendatahub.io"

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
		pc := MonitorCRD(testCRDName)
		result, err := pc.check(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.Pass).To(BeTrue())
	})

	t.Run("MonitorCRDs", func(t *testing.T) {
		g := NewWithT(t)
		pc := MonitorCRDs([]string{testCRDName})
		result, err := pc.check(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.Pass).To(BeTrue())
	})
}

func TestMonitorCRDs_AllPresent(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	cli := envTest.Client()

	crd1, err := envTest.RegisterCRD(ctx, testCRDGVK, "testpreconditionresources", "testpreconditionresource", apiextensionsv1.ClusterScoped)
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd1)

	crd2, err := envTest.RegisterCRD(ctx, testCRDGVK2, "testpreconditionresource2s", "testpreconditionresource2", apiextensionsv1.ClusterScoped)
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd2)

	rr := &types.ReconciliationRequest{Client: cli}

	pc := MonitorCRDs([]string{testCRDName, testCRDName2})
	result, err := pc.check(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Pass).To(BeTrue())
}

func TestMonitorCRD_Absent(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	pc := MonitorCRD(absentCRDName)
	result, err := pc.check(ctx, &types.ReconciliationRequest{Client: envTest.Client()})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Pass).To(BeFalse())
	g.Expect(result.Message).To(ContainSubstring(absentCRDName))
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

	pc := MonitorCRDs([]string{testCRDName, absentCRDName})
	result, err := pc.check(ctx, &types.ReconciliationRequest{Client: cli})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Pass).To(BeFalse())
	g.Expect(result.Message).To(ContainSubstring(absentCRDName))
	g.Expect(result.Message).NotTo(ContainSubstring(testCRDName))
}

func TestMonitorCRDs_EmptySlice(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	rr := &types.ReconciliationRequest{Client: envTest.Client()}

	pc := MonitorCRDs(nil)
	result, checkErr := pc.check(ctx, rr)

	g.Expect(checkErr).To(HaveOccurred())
	g.Expect(checkErr.Error()).To(ContainSubstring("empty required API list"))
	g.Expect(result.Pass).To(BeFalse())
}

func TestMonitorAPIs_ServedVersion(t *testing.T) {
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

	t.Run("served version passes", func(t *testing.T) {
		g := NewWithT(t)

		pc := MonitorAPIs([]RequiredAPI{{CRDName: testCRDName, GVK: testCRDGVK}})
		result, err := pc.check(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.ConditionStatus()).To(Equal(metav1.ConditionTrue))
	})

	t.Run("unserved version fails", func(t *testing.T) {
		g := NewWithT(t)

		unserved := testCRDGVK
		unserved.Version = "v99"

		pc := MonitorAPIs([]RequiredAPI{{CRDName: testCRDName, GVK: unserved}})
		result, err := pc.check(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.ConditionStatus()).To(Equal(metav1.ConditionFalse))
		g.Expect(result.Message).To(ContainSubstring("does not serve version v99"))
	})
}

func TestSkipIfCRDAbsent(t *testing.T) {
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

	t.Run("present CRD does not skip", func(t *testing.T) {
		g := NewWithT(t)

		skip, err := SkipIfCRDAbsent(testCRDName)(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(skip).To(BeFalse())
	})

	t.Run("absent CRD skips", func(t *testing.T) {
		g := NewWithT(t)

		skip, err := SkipIfCRDAbsent(absentCRDName)(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(skip).To(BeTrue())
	})
}

func TestSkipIfCRDAbsent_IntegrationWithRunAll(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	cli := envTest.Client()

	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
	rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

	pcs := []PreCondition{MonitorCRD(absentCRDName, WithSkipFunc(SkipIfCRDAbsent(absentCRDName)))}

	g.Expect(RunAll(ctx, rr, pcs)).To(BeFalse())

	got := condManager.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionTrue))
}

func TestMonitorCRD_NotEstablishedIsUnknown(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	cli, err := fakeclient.New(fakeclient.WithObjects(&apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: testCRDName},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{{
				Type:   apiextensionsv1.Established,
				Status: apiextensionsv1.ConditionFalse,
			}},
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())

	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
	rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

	g.Expect(RunAll(ctx, rr, []PreCondition{MonitorCRD(testCRDName)})).To(BeFalse())

	got := condManager.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(got.Message).To(ContainSubstring("CRD is not established"))
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

	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
	rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

	pcs := []PreCondition{
		MonitorCRD(testCRDName),
		MonitorCRD(absentCRDName),
	}

	shouldStop := RunAll(ctx, rr, pcs)
	g.Expect(shouldStop).To(BeFalse())

	got := condManager.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got.Message).To(ContainSubstring(absentCRDName))
	g.Expect(got.Message).NotTo(ContainSubstring(testCRDName))
}

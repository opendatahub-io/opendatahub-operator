//nolint:testpackage
package precondition

import (
	"context"
	"testing"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

var testOperatorGVK = schema.GroupVersionKind{
	Group:   "test.opendatahub.io",
	Version: "v1",
	Kind:    "TestOperator",
}

var testClusterOperatorGVK = schema.GroupVersionKind{
	Group:   "test.opendatahub.io",
	Version: "v1",
	Kind:    "TestClusterOperator",
}

func defaultTestFilter(condType, condStatus string) bool {
	if condType == "Degraded" && condStatus == string(metav1.ConditionTrue) {
		return true
	}
	if (condType == "Available" || condType == "Ready") && condStatus == string(metav1.ConditionFalse) {
		return true
	}

	return false
}

func TestMonitorOperator(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	crd, err := envTest.RegisterCRD(ctx, testOperatorGVK, "testoperators", "testoperator", apiextensionsv1.NamespaceScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)

	tests := []struct {
		name                   string
		setupOperatorCR        func(g *WithT, ns string) *unstructured.Unstructured
		filter                 ConditionFilterFunc
		expectedStatus         metav1.ConditionStatus
		expectedMsgContains    []string
		expectedMsgNotContains []string
	}{
		{
			name: "Degraded=True",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setOperatorCondition(g, cr, "Degraded", "True", "TestFailed", "Test failure message")
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:              defaultTestFilter,
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{testOperatorGVK.Kind, "Degraded=True", "TestFailed", "Test failure message"},
		},
		{
			name: "Ready=False",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setOperatorCondition(g, cr, "Ready", "False", "NotReady", "Waiting for something")
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:              defaultTestFilter,
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{testOperatorGVK.Kind, "Ready=False", "NotReady", "Waiting for something"},
		},
		{
			name: "Available=False",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setOperatorCondition(g, cr, "Available", "False", "Unavailable", "Service unavailable")
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:              defaultTestFilter,
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{testOperatorGVK.Kind, "Available=False", "Unavailable", "Service unavailable"},
		},
		{
			name: "healthy operator (Ready=True)",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setOperatorCondition(g, cr, "Ready", "True", "Ready", "All good")
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:         defaultTestFilter,
			expectedStatus: metav1.ConditionTrue,
		},
		{
			name: "healthy operator (Degraded=False)",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setOperatorCondition(g, cr, "Degraded", "False", "AsExpected", "All is well")
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:         defaultTestFilter,
			expectedStatus: metav1.ConditionTrue,
		},
		{
			name: "operator CR not found",
			setupOperatorCR: func(_ *WithT, _ string) *unstructured.Unstructured {
				return nil
			},
			filter:         defaultTestFilter,
			expectedStatus: metav1.ConditionTrue,
		},
		{
			name: "operator without conditions",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				g.Expect(cli.Create(ctx, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:         defaultTestFilter,
			expectedStatus: metav1.ConditionTrue,
		},
		{
			name: "custom filter",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setOperatorCondition(g, cr, "CustomError", "True", "ErrorOccurred", "Custom error")
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter: func(condType, condStatus string) bool {
				return condType == "CustomError" && condStatus == "True"
			},
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{testOperatorGVK.Kind, "CustomError=True", "ErrorOccurred", "Custom error"},
		},
		{
			name: "multiple degraded conditions",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setMultipleOperatorConditions(g, cr, []metav1.Condition{
					{Type: "Degraded", Status: metav1.ConditionTrue, Reason: "Error1", Message: "First error"},
					{Type: "Available", Status: metav1.ConditionFalse, Reason: "Unavail", Message: "Second error"},
					{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready", Message: "Healthy"},
				})
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:                 defaultTestFilter,
			expectedStatus:         metav1.ConditionFalse,
			expectedMsgContains:    []string{"Degraded=True", "First error", "Available=False", "Second error"},
			expectedMsgNotContains: []string{"Ready=True"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			nsn := xid.New().String()

			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsn}}
			g.Expect(cli.Create(ctx, &ns)).NotTo(HaveOccurred())
			t.Cleanup(func() { _ = cli.Delete(ctx, &ns) })

			var operatorCR *unstructured.Unstructured
			if tt.setupOperatorCR != nil {
				operatorCR = tt.setupOperatorCR(g, nsn)
			}
			t.Cleanup(func() {
				if operatorCR != nil {
					_ = cli.Delete(ctx, operatorCR)
				}
			})

			config := OperatorConfig{
				OperatorGVK: testOperatorGVK,
				CRNamespace: nsn,
				Filter:      tt.filter,
			}
			if operatorCR != nil {
				config.CRName = operatorCR.GetName()
			} else {
				config.CRName = "missing-operator"
			}

			instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
			condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
			rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

			pcs := []PreCondition{MonitorOperator(config)}
			RunAll(ctx, rr, pcs)

			got := condManager.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(got).NotTo(BeNil())
			g.Expect(got.Status).To(Equal(tt.expectedStatus))

			for _, s := range tt.expectedMsgContains {
				g.Expect(got.Message).To(ContainSubstring(s))
			}
			for _, s := range tt.expectedMsgNotContains {
				g.Expect(got.Message).NotTo(ContainSubstring(s))
			}
		})
	}
}

func TestMonitorOperator_MissingCRD(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()

	nonExistentGVK := schema.GroupVersionKind{
		Group:   "nonexistent.opendatahub.io",
		Version: "v1",
		Kind:    "NonExistentOperator",
	}

	tests := []struct {
		name   string
		crName string
	}{
		{"with CRName specified", "any-name"},
		{"with CRName empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
			condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
			rr := &types.ReconciliationRequest{Client: envTest.Client(), Instance: instance, Conditions: condManager}

			pcs := []PreCondition{MonitorOperator(OperatorConfig{
				OperatorGVK: nonExistentGVK,
				CRName:      tt.crName,
				CRNamespace: "any-namespace",
				Filter:      defaultTestFilter,
			})}
			RunAll(ctx, rr, pcs)

			got := condManager.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(got).NotTo(BeNil())
			g.Expect(got.Status).To(Equal(metav1.ConditionTrue))
		})
	}
}

func TestMonitorOperator_FirstCRDiscovery(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	crd, err := envTest.RegisterCRD(ctx, testOperatorGVK, "testoperators", "testoperator", apiextensionsv1.NamespaceScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)

	crd, err = envTest.RegisterCRD(ctx, testClusterOperatorGVK, "testclusteroperators", "testclusteroperator", apiextensionsv1.ClusterScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)

	t.Run("namespace-scoped CR discovered without CRName", func(t *testing.T) {
		g := NewWithT(t)

		nsn := xid.New().String()
		ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsn}}
		g.Expect(cli.Create(ctx, &ns)).NotTo(HaveOccurred())
		t.Cleanup(func() { _ = cli.Delete(ctx, &ns) })

		cr := testf.NewUnstructuredCR(xid.New().String(), nsn, testOperatorGVK)
		setOperatorCondition(g, cr, "Degraded", "True", "TestReason", "Test message")
		g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())
		t.Cleanup(func() { _ = cli.Delete(ctx, cr) })

		instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
		condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
		rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

		pcs := []PreCondition{MonitorOperator(OperatorConfig{
			OperatorGVK: testOperatorGVK,
			CRNamespace: nsn,
			Filter:      defaultTestFilter,
		})}
		RunAll(ctx, rr, pcs)

		got := condManager.GetCondition(status.ConditionDependenciesAvailable)
		g.Expect(got).NotTo(BeNil())
		g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(got.Message).To(ContainSubstring("Degraded=True"))
		g.Expect(got.Message).To(ContainSubstring("TestReason"))
	})

	t.Run("cluster-scoped CR discovered without CRName", func(t *testing.T) {
		g := NewWithT(t)

		cr := testf.NewUnstructuredCR(xid.New().String(), "", testClusterOperatorGVK)
		setOperatorCondition(g, cr, "Degraded", "True", "ClusterFailed", "Cluster-scoped failure")
		g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())
		t.Cleanup(func() { _ = cli.Delete(ctx, cr) })

		instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
		condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
		rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

		// CRNamespace is intentionally empty — this exercises the cluster-scoped list path.
		pcs := []PreCondition{MonitorOperator(OperatorConfig{
			OperatorGVK: testClusterOperatorGVK,
			Filter:      defaultTestFilter,
		})}
		RunAll(ctx, rr, pcs)

		got := condManager.GetCondition(status.ConditionDependenciesAvailable)
		g.Expect(got).NotTo(BeNil())
		g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(got.Message).To(ContainSubstring("Degraded=True"))
		g.Expect(got.Message).To(ContainSubstring("ClusterFailed"))
	})

	t.Run("multiple CRs uses first found", func(t *testing.T) {
		g := NewWithT(t)

		nsn := xid.New().String()
		ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsn}}
		g.Expect(cli.Create(ctx, &ns)).NotTo(HaveOccurred())
		t.Cleanup(func() { _ = cli.Delete(ctx, &ns) })

		cr1 := testf.NewUnstructuredCR(xid.New().String(), nsn, testOperatorGVK)
		setOperatorCondition(g, cr1, "Degraded", "True", "FirstCR", "First CR is degraded")
		g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr1)).NotTo(HaveOccurred())
		t.Cleanup(func() { _ = cli.Delete(ctx, cr1) })

		cr2 := testf.NewUnstructuredCR(xid.New().String(), nsn, testOperatorGVK)
		g.Expect(cli.Create(ctx, cr2)).NotTo(HaveOccurred())
		t.Cleanup(func() { _ = cli.Delete(ctx, cr2) })

		instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
		condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
		rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

		pcs := []PreCondition{MonitorOperator(OperatorConfig{
			OperatorGVK: testOperatorGVK,
			CRNamespace: nsn,
			Filter:      defaultTestFilter,
		})}
		RunAll(ctx, rr, pcs)

		got := condManager.GetCondition(status.ConditionDependenciesAvailable)
		g.Expect(got).NotTo(BeNil())
		// With multiple CRs, the precondition should still complete (not error).
		// The exact CR selected may be arbitrary, so we just verify the check ran.
		g.Expect(got.Status).NotTo(Equal(metav1.ConditionUnknown))
	})
}

func TestMonitorOperator_MalformedConditions(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	crd, err := envTest.RegisterCRD(ctx, testOperatorGVK, "testoperators", "testoperator", apiextensionsv1.NamespaceScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)

	nsn := xid.New().String()
	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsn}}
	g.Expect(cli.Create(ctx, &ns)).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = cli.Delete(ctx, &ns) })

	t.Run("conditions field is not a slice", func(t *testing.T) {
		g := NewWithT(t)

		cr := testf.NewUnstructuredCR(xid.New().String(), nsn, testOperatorGVK)
		g.Expect(cli.Create(ctx, cr)).NotTo(HaveOccurred())
		t.Cleanup(func() { _ = cli.Delete(ctx, cr) })

		// Set status.conditions to a string instead of a slice
		g.Expect(unstructured.SetNestedField(cr.Object, "not-a-slice", "status", "conditions")).NotTo(HaveOccurred())
		g.Expect(cli.Status().Update(ctx, cr)).NotTo(HaveOccurred())

		instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
		condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
		rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

		pcs := []PreCondition{MonitorOperator(OperatorConfig{
			OperatorGVK: testOperatorGVK,
			CRName:      cr.GetName(),
			CRNamespace: nsn,
			Filter:      defaultTestFilter,
		})}
		RunAll(ctx, rr, pcs)

		got := condManager.GetCondition(status.ConditionDependenciesAvailable)
		g.Expect(got).NotTo(BeNil())
		g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
		g.Expect(got.Message).To(ContainSubstring("failed to parse conditions"))
	})
}

func TestMonitorOperator_Severity(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	crd, err := envTest.RegisterCRD(ctx, testOperatorGVK, "testoperators", "testoperator", apiextensionsv1.NamespaceScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)

	nsn := xid.New().String()
	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsn}}
	g.Expect(cli.Create(ctx, &ns)).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = cli.Delete(ctx, &ns) })

	cr := testf.NewUnstructuredCR(xid.New().String(), nsn, testOperatorGVK)
	setOperatorCondition(g, cr, "Degraded", "True", "InfoFailed", "Info-level failure")
	g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = cli.Delete(ctx, cr) })

	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
	rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

	pcs := []PreCondition{MonitorOperator(OperatorConfig{
		OperatorGVK: testOperatorGVK,
		CRName:      cr.GetName(),
		CRNamespace: nsn,
		Filter:      defaultTestFilter,
	}, WithSeverity(common.ConditionSeverityInfo))}
	RunAll(ctx, rr, pcs)

	got := condManager.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got.Severity).To(Equal(common.ConditionSeverityInfo))
}

func TestMonitorOperator_NilFilter(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	crd, err := envTest.RegisterCRD(ctx, testOperatorGVK, "testoperators", "testoperator", apiextensionsv1.NamespaceScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)

	nsn := xid.New().String()
	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsn}}
	g.Expect(cli.Create(ctx, &ns)).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = cli.Delete(ctx, &ns) })

	cr := testf.NewUnstructuredCR(xid.New().String(), nsn, testOperatorGVK)
	setOperatorCondition(g, cr, "Degraded", "True", "Failed", "Failure")
	g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = cli.Delete(ctx, cr) })

	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
	rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

	pcs := []PreCondition{MonitorOperator(OperatorConfig{
		OperatorGVK: testOperatorGVK,
		CRName:      cr.GetName(),
		CRNamespace: nsn,
	})}
	RunAll(ctx, rr, pcs)

	got := condManager.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(got.Message).To(ContainSubstring("Filter must not be nil"))
}

// Test helpers

func setOperatorCondition(g *WithT, cr *unstructured.Unstructured, cType, condStatus, reason, message string) {
	g.THelper()

	err := testf.SetTypedConditions(cr, []metav1.Condition{{
		Type:               cType,
		Status:             metav1.ConditionStatus(condStatus),
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}})
	g.Expect(err).NotTo(HaveOccurred())
}

func setMultipleOperatorConditions(g *WithT, cr *unstructured.Unstructured, conditions []metav1.Condition) {
	g.THelper()

	err := testf.SetTypedConditions(cr, conditions)
	g.Expect(err).NotTo(HaveOccurred())
}

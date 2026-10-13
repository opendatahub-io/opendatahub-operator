package monitor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/monitor"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

var testOperatorGVK = schema.GroupVersionKind{
	Group:   "test.monitor.io",
	Version: "v1",
	Kind:    "TestOperator",
}

var testClusterOperatorGVK = schema.GroupVersionKind{
	Group:   "test.monitor.io",
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

func TestCheckOperatorHealth(t *testing.T) {
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
		filter                 monitor.ConditionFilterFunc
		requireCR              bool
		expectedPass           bool
		expectedMsgContains    []string
		expectedMsgNotContains []string
	}{
		{
			name: "Degraded=True fails",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setOperatorCondition(g, cr, "Degraded", "True", "TestFailed", "Test failure message")
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:              defaultTestFilter,
			expectedPass:        false,
			expectedMsgContains: []string{testOperatorGVK.Kind, "Degraded=True", "TestFailed", "Test failure message"},
		},
		{
			name: "Ready=False fails",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setOperatorCondition(g, cr, "Ready", "False", "NotReady", "Waiting for something")
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:              defaultTestFilter,
			expectedPass:        false,
			expectedMsgContains: []string{testOperatorGVK.Kind, "Ready=False", "NotReady", "Waiting for something"},
		},
		{
			name: "Available=False fails",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setOperatorCondition(g, cr, "Available", "False", "Unavailable", "Service unavailable")
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:              defaultTestFilter,
			expectedPass:        false,
			expectedMsgContains: []string{testOperatorGVK.Kind, "Available=False", "Unavailable", "Service unavailable"},
		},
		{
			name: "healthy operator Ready=True passes",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setOperatorCondition(g, cr, "Ready", "True", "Ready", "All good")
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:       defaultTestFilter,
			expectedPass: true,
		},
		{
			name: "healthy operator Degraded=False passes",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				setOperatorCondition(g, cr, "Degraded", "False", "AsExpected", "All is well")
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:       defaultTestFilter,
			expectedPass: true,
		},
		{
			name: "operator CR not found passes",
			setupOperatorCR: func(_ *WithT, _ string) *unstructured.Unstructured {
				return nil
			},
			filter:       defaultTestFilter,
			expectedPass: true,
		},
		{
			name: "operator CR not found with RequireCR fails",
			setupOperatorCR: func(_ *WithT, _ string) *unstructured.Unstructured {
				return nil
			},
			filter:              defaultTestFilter,
			requireCR:           true,
			expectedPass:        false,
			expectedMsgContains: []string{"operator CR not found"},
		},
		{
			name: "operator without conditions passes",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				cr := testf.NewUnstructuredCR(xid.New().String(), ns, testOperatorGVK)
				g.Expect(cli.Create(ctx, cr)).NotTo(HaveOccurred())

				return cr
			},
			filter:       defaultTestFilter,
			expectedPass: true,
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
			expectedPass:        false,
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
			expectedPass:           false,
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

			config := monitor.OperatorConfig{
				OperatorGVK: testOperatorGVK,
				CRNamespace: nsn,
				Filter:      tt.filter,
				RequireCR:   tt.requireCR,
			}
			if operatorCR != nil {
				config.CRName = operatorCR.GetName()
			} else {
				config.CRName = "missing-operator"
			}

			result, err := monitor.CheckOperatorHealth(ctx, cli, config)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Pass).To(Equal(tt.expectedPass))

			for _, s := range tt.expectedMsgContains {
				g.Expect(result.Message).To(ContainSubstring(s))
			}
			for _, s := range tt.expectedMsgNotContains {
				g.Expect(result.Message).NotTo(ContainSubstring(s))
			}
		})
	}
}

func TestCheckOperatorHealth_MissingCRD(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()

	nonExistentGVK := schema.GroupVersionKind{
		Group:   "nonexistent.monitor.io",
		Version: "v1",
		Kind:    "NonExistentOperator",
	}

	tests := []struct {
		name      string
		crName    string
		requireCR bool
	}{
		{"with CRName specified", "any-name", false},
		{"with CRName empty", "", false},
		{"RequireCR=true still passes when CRD is missing", "any-name", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			result, err := monitor.CheckOperatorHealth(ctx, envTest.Client(), monitor.OperatorConfig{
				OperatorGVK: nonExistentGVK,
				CRName:      tt.crName,
				CRNamespace: "any-namespace",
				Filter:      defaultTestFilter,
				RequireCR:   tt.requireCR,
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Pass).To(BeTrue())
		})
	}
}

func TestCheckOperatorHealth_NoMatchPolicy(t *testing.T) {
	g := NewWithT(t)
	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return &meta.NoKindMatchError{GroupKind: testOperatorGVK.GroupKind(), SearchedVersions: []string{testOperatorGVK.Version}}
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())

	for _, tc := range []struct {
		name           string
		noMatchUnknown bool
		want           metav1.ConditionStatus
	}{
		{name: "optional API is skipped", want: metav1.ConditionTrue},
		{name: "known-present API is indeterminate", noMatchUnknown: true, want: metav1.ConditionUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			result, err := monitor.CheckOperatorHealth(t.Context(), cli, monitor.OperatorConfig{
				OperatorGVK:      testOperatorGVK,
				CRName:           "cluster",
				RequireCR:        true,
				NoMatchAsUnknown: tc.noMatchUnknown,
				Filter:           defaultTestFilter,
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.ConditionStatus()).To(Equal(tc.want))
		})
	}
}

func TestCheckOperatorHealth_FirstCRDiscovery(t *testing.T) {
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

		result, err := monitor.CheckOperatorHealth(ctx, cli, monitor.OperatorConfig{
			OperatorGVK: testOperatorGVK,
			CRNamespace: nsn,
			Filter:      defaultTestFilter,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.Pass).To(BeFalse())
		g.Expect(result.Message).To(ContainSubstring("Degraded=True"))
		g.Expect(result.Message).To(ContainSubstring("TestReason"))
	})

	t.Run("cluster-scoped CR discovered without CRName", func(t *testing.T) {
		g := NewWithT(t)

		cr := testf.NewUnstructuredCR(xid.New().String(), "", testClusterOperatorGVK)
		setOperatorCondition(g, cr, "Degraded", "True", "ClusterFailed", "Cluster-scoped failure")
		g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())
		t.Cleanup(func() { _ = cli.Delete(ctx, cr) })

		result, err := monitor.CheckOperatorHealth(ctx, cli, monitor.OperatorConfig{
			OperatorGVK: testClusterOperatorGVK,
			Filter:      defaultTestFilter,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.Pass).To(BeFalse())
		g.Expect(result.Message).To(ContainSubstring("Degraded=True"))
		g.Expect(result.Message).To(ContainSubstring("ClusterFailed"))
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

		result, err := monitor.CheckOperatorHealth(ctx, cli, monitor.OperatorConfig{
			OperatorGVK: testOperatorGVK,
			CRNamespace: nsn,
			Filter:      defaultTestFilter,
		})
		g.Expect(err).NotTo(HaveOccurred())
		// With multiple CRs, the check should still complete (not error).
		g.Expect(result.Pass).NotTo(BeNil())
	})
}

func TestCheckOperatorHealth_MalformedConditions(t *testing.T) {
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

		g.Expect(unstructured.SetNestedField(cr.Object, "not-a-slice", "status", "conditions")).NotTo(HaveOccurred())
		g.Expect(cli.Status().Update(ctx, cr)).NotTo(HaveOccurred())

		result, err := monitor.CheckOperatorHealth(ctx, cli, monitor.OperatorConfig{
			OperatorGVK: testOperatorGVK,
			CRName:      cr.GetName(),
			CRNamespace: nsn,
			Filter:      defaultTestFilter,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.ConditionStatus()).To(Equal(metav1.ConditionUnknown))
		g.Expect(result.Message).To(ContainSubstring("failed to parse status conditions"))
	})

	t.Run("duplicate condition types are indeterminate", func(t *testing.T) {
		g := NewWithT(t)

		cr := testf.NewUnstructuredCR(xid.New().String(), nsn, testOperatorGVK)
		setMultipleOperatorConditions(g, cr, []metav1.Condition{
			{Type: "Degraded", Status: metav1.ConditionTrue, Reason: "Broken", LastTransitionTime: metav1.Now()},
			{Type: "Degraded", Status: metav1.ConditionFalse, Reason: "AsExpected", LastTransitionTime: metav1.Now()},
		})
		g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())
		t.Cleanup(func() { _ = cli.Delete(ctx, cr) })

		result, err := monitor.CheckOperatorHealth(ctx, cli, monitor.OperatorConfig{
			OperatorGVK: testOperatorGVK,
			CRName:      cr.GetName(),
			CRNamespace: nsn,
			Filter:      defaultTestFilter,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.ConditionStatus()).To(Equal(metav1.ConditionUnknown))
		g.Expect(result.Message).To(ContainSubstring("duplicate condition type \"Degraded\""))
	})
}

func TestCheckOperatorHealth_NoCriteria(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())

	_, err = monitor.CheckOperatorHealth(ctx, cli, monitor.OperatorConfig{
		OperatorGVK: testOperatorGVK,
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("at least one of Filter or RequiredConditions must be set"))
}

func TestCheckOperatorHealth_InvalidRequiredCondition(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		condition monitor.RequiredCondition
	}{
		{name: "empty type", condition: monitor.RequiredCondition{Status: "True"}},
		{name: "empty status", condition: monitor.RequiredCondition{Type: "Available"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			cli, err := fakeclient.New()
			g.Expect(err).NotTo(HaveOccurred())

			_, err = monitor.CheckOperatorHealth(ctx, cli, monitor.OperatorConfig{
				OperatorGVK:        testOperatorGVK,
				RequiredConditions: []monitor.RequiredCondition{tt.condition},
			})
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring("RequiredCondition Type and Status must not be empty"))
		})
	}
}

func TestCheckOperatorHealth_RequiredConditions(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	crd, err := envTest.RegisterCRD(ctx, testOperatorGVK, "testoperators", "testoperator", apiextensionsv1.NamespaceScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)

	required := []monitor.RequiredCondition{
		{Type: "Available", Status: string(metav1.ConditionTrue)},
		{Type: "Degraded", Status: string(metav1.ConditionFalse)},
	}

	tests := []struct {
		name                string
		conditions          []metav1.Condition
		createCR            bool
		expectedStatus      metav1.ConditionStatus
		expectedMsgContains []string
	}{
		{
			name: "all required conditions met passes",
			conditions: []metav1.Condition{
				{Type: "Available", Status: metav1.ConditionTrue, Reason: "AsExpected"},
				{Type: "Degraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
			},
			createCR:       true,
			expectedStatus: metav1.ConditionTrue,
		},
		{
			name: "Available=False fails",
			conditions: []metav1.Condition{
				{Type: "Available", Status: metav1.ConditionFalse, Reason: "Unavailable", Message: "no pods"},
				{Type: "Degraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
			},
			createCR:            true,
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{"Available=False", "no pods", "expected Available=True"},
		},
		{
			name: "Degraded=True fails",
			conditions: []metav1.Condition{
				{Type: "Available", Status: metav1.ConditionTrue, Reason: "AsExpected"},
				{Type: "Degraded", Status: metav1.ConditionTrue, Reason: "Broken", Message: "reconcile failed"},
			},
			createCR:            true,
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{"Degraded=True", "reconcile failed", "expected Degraded=False"},
		},
		{
			name: "missing required condition is Unknown",
			conditions: []metav1.Condition{
				{Type: "Degraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
			},
			createCR:            true,
			expectedStatus:      metav1.ConditionUnknown,
			expectedMsgContains: []string{"required condition Available not found"},
		},
		{
			name:                "no conditions at all is Unknown",
			conditions:          nil,
			createCR:            true,
			expectedStatus:      metav1.ConditionUnknown,
			expectedMsgContains: []string{"required condition Available not found", "required condition Degraded not found"},
		},
		{
			name: "required condition reporting Unknown is Unknown",
			conditions: []metav1.Condition{
				{Type: "Available", Status: metav1.ConditionUnknown, Reason: "Initializing", Message: "starting up"},
				{Type: "Degraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
			},
			createCR:            true,
			expectedStatus:      metav1.ConditionUnknown,
			expectedMsgContains: []string{"Available=Unknown", "starting up"},
		},
		{
			name: "malformed required condition status is Unknown",
			conditions: []metav1.Condition{
				{Type: "Available", Status: metav1.ConditionStatus("garbled"), Reason: "Broken"},
				{Type: "Degraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
			},
			createCR:            true,
			expectedStatus:      metav1.ConditionUnknown,
			expectedMsgContains: []string{"Available=garbled"},
		},
		{
			name: "failure and indeterminate together report False with both findings",
			conditions: []metav1.Condition{
				{Type: "Degraded", Status: metav1.ConditionTrue, Reason: "Broken", Message: "reconcile failed"},
			},
			createCR:            true,
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{"Degraded=True", "required condition Available not found"},
		},
		{
			name:                "missing CR with RequireCR fails",
			createCR:            false,
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{"operator CR not found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			nsn := xid.New().String()
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsn}}
			g.Expect(cli.Create(ctx, &ns)).NotTo(HaveOccurred())
			t.Cleanup(func() { _ = cli.Delete(ctx, &ns) })

			crName := "missing-operator"

			if tt.createCR {
				cr := testf.NewUnstructuredCR(xid.New().String(), nsn, testOperatorGVK)
				if len(tt.conditions) > 0 {
					setMultipleOperatorConditions(g, cr, tt.conditions)
					g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())
				} else {
					g.Expect(cli.Create(ctx, cr)).NotTo(HaveOccurred())
				}
				t.Cleanup(func() { _ = cli.Delete(ctx, cr) })

				crName = cr.GetName()
			}

			result, err := monitor.CheckOperatorHealth(ctx, cli, monitor.OperatorConfig{
				OperatorGVK:        testOperatorGVK,
				CRName:             crName,
				CRNamespace:        nsn,
				RequireCR:          true,
				RequiredConditions: required,
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.ConditionStatus()).To(Equal(tt.expectedStatus))

			for _, s := range tt.expectedMsgContains {
				g.Expect(result.Message).To(ContainSubstring(s))
			}
		})
	}
}

func TestCheckOperatorHealth_EmptyGVK(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())

	_, err = monitor.CheckOperatorHealth(ctx, cli, monitor.OperatorConfig{
		Filter: defaultTestFilter,
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("OperatorGVK must not be empty"))
}

func TestCheckOperatorHealth_TransientAPIError(t *testing.T) {
	ctx := context.Background()

	apiErr := errors.New("simulated transient API error")
	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return apiErr
		},
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return apiErr
		},
	}))

	NewWithT(t).Expect(err).NotTo(HaveOccurred())

	tests := []struct {
		name   string
		crName string
	}{
		{"Get error with CRName", "some-cr"},
		{"List error without CRName", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			_, err := monitor.CheckOperatorHealth(ctx, cli, monitor.OperatorConfig{
				OperatorGVK: testOperatorGVK,
				CRName:      tt.crName,
				Filter:      defaultTestFilter,
			})
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring("failed to get operator CR"))
		})
	}
}

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

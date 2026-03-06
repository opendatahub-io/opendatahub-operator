package dependency_test

import (
	"context"
	"testing"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

//nolint:gochecknoinits
func init() {
	log.SetLogger(zap.New(zap.UseDevMode(true)))
}

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

func TestDependencyAction(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	ctx := context.Background()
	cli := envTest.Client()

	// Create CRD once for all test cases
	createTestOperatorCRD(t, g, ctx, envTest)

	tests := []struct {
		name                   string
		setupOperatorCR        func(g *WithT, ns string) *unstructured.Unstructured
		filter                 dependency.DegradedConditionFilterFunc
		severity               common.ConditionSeverity
		expectedAvailable      bool
		expectedMsgContains    []string
		expectedMsgNotContains []string
	}{
		{
			name: "Degraded=True",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				operatorCR := createOperatorCR(xid.New().String(), ns, testOperatorGVK)
				setCondition(g, operatorCR, "Degraded", "True", "TestFailed", "Test failure message")
				createAndUpdateStatus(ctx, g, cli, operatorCR)
				return operatorCR
			},
			severity:            common.ConditionSeverityError,
			expectedAvailable:   false,
			expectedMsgContains: []string{"Dependencies degraded", testOperatorGVK.Kind, "Degraded=True", "TestFailed", "Test failure message"},
		},
		{
			name: "Ready=False",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				operatorCR := createOperatorCR(xid.New().String(), ns, testOperatorGVK)
				setCondition(g, operatorCR, "Ready", "False", "NotReady", "Waiting for something")
				createAndUpdateStatus(ctx, g, cli, operatorCR)
				return operatorCR
			},
			severity:            common.ConditionSeverityError,
			expectedAvailable:   false,
			expectedMsgContains: []string{"Dependencies degraded", testOperatorGVK.Kind, "Ready=False", "NotReady", "Waiting for something"},
		},
		{
			name: "Available=False",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				operatorCR := createOperatorCR(xid.New().String(), ns, testOperatorGVK)
				setCondition(g, operatorCR, "Available", "False", "Unavailable", "Service unavailable")
				createAndUpdateStatus(ctx, g, cli, operatorCR)
				return operatorCR
			},
			severity:            common.ConditionSeverityError,
			expectedAvailable:   false,
			expectedMsgContains: []string{"Dependencies degraded", testOperatorGVK.Kind, "Available=False", "Unavailable", "Service unavailable"},
		},
		{
			name: "healthy (Ready=True)",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				operatorCR := createOperatorCR(xid.New().String(), ns, testOperatorGVK)
				setCondition(g, operatorCR, "Ready", "True", "Ready", "All good")
				createAndUpdateStatus(ctx, g, cli, operatorCR)
				return operatorCR
			},
			severity:          common.ConditionSeverityError,
			expectedAvailable: true,
		},
		{
			name: "healthy (Degraded=False)",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				operatorCR := createOperatorCR(xid.New().String(), ns, testOperatorGVK)
				setCondition(g, operatorCR, "Degraded", "False", "AsExpected", "All is well")
				createAndUpdateStatus(ctx, g, cli, operatorCR)
				return operatorCR
			},
			severity:          common.ConditionSeverityError,
			expectedAvailable: true,
		},
		{
			name: "operator not found",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				return nil
			},
			severity:          common.ConditionSeverityError,
			expectedAvailable: true,
		},
		{
			name: "operator without conditions",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				operatorCR := createOperatorCR(xid.New().String(), ns, testOperatorGVK)
				err := cli.Create(ctx, operatorCR)
				g.Expect(err).NotTo(HaveOccurred())
				return operatorCR
			},
			severity:          common.ConditionSeverityError,
			expectedAvailable: true,
		},
		{
			name: "custom filter",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				operatorCR := createOperatorCR(xid.New().String(), ns, testOperatorGVK)
				setCondition(g, operatorCR, "CustomError", "True", "ErrorOccurred", "Custom error")
				createAndUpdateStatus(ctx, g, cli, operatorCR)
				return operatorCR
			},
			filter: func(condType, status string) bool {
				return condType == "CustomError" && status == "True"
			},
			severity:            common.ConditionSeverityError,
			expectedAvailable:   false,
			expectedMsgContains: []string{"Dependencies degraded", testOperatorGVK.Kind, "CustomError=True", "ErrorOccurred", "Custom error"},
		},
		{
			name: "multiple degraded conditions",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				operatorCR := createOperatorCR(xid.New().String(), ns, testOperatorGVK)
				setMultipleConditions(g, operatorCR, []metav1.Condition{
					{Type: "Degraded", Status: metav1.ConditionTrue, Reason: "Error1", Message: "First error"},
					{Type: "Available", Status: metav1.ConditionFalse, Reason: "Unavail", Message: "Second error"},
					{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready", Message: "Healthy"},
				})
				createAndUpdateStatus(ctx, g, cli, operatorCR)
				return operatorCR
			},
			severity:               common.ConditionSeverityError,
			expectedAvailable:      false,
			expectedMsgContains:    []string{"Dependencies degraded", "Degraded=True", "First error", "Available=False", "Second error"},
			expectedMsgNotContains: []string{"Ready=True"},
		},
		{
			name: "info severity",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				operatorCR := createOperatorCR(xid.New().String(), ns, testOperatorGVK)
				setCondition(g, operatorCR, "Degraded", "True", "InfoFailed", "Info-level failure")
				createAndUpdateStatus(ctx, g, cli, operatorCR)
				return operatorCR
			},
			severity:            common.ConditionSeverityInfo,
			expectedAvailable:   false,
			expectedMsgContains: []string{"Dependencies degraded", "Degraded=True"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			nsn := xid.New().String()

			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsn,
				},
			}
			g.Expect(cli.Create(ctx, &ns)).NotTo(HaveOccurred())

			var operatorCR *unstructured.Unstructured
			if tt.setupOperatorCR != nil {
				operatorCR = tt.setupOperatorCR(g, nsn)
			}

			t.Cleanup(func() {
				if operatorCR != nil {
					_ = cli.Delete(ctx, operatorCR)
				}
				_ = cli.Delete(ctx, &ns)
			})

			instance := &componentApi.Kueue{
				ObjectMeta: metav1.ObjectMeta{
					Name: xid.New().String(),
				},
			}

			config := dependency.OperatorConfig{
				OperatorGVK: testOperatorGVK,
				CRNamespace: nsn,
				Filter:      tt.filter,
				Severity:    tt.severity,
			}
			if operatorCR != nil {
				config.CRName = operatorCR.GetName()
			} else {
				config.CRName = "missing-operator"
			}

			condManager := cond.NewManager(instance, status.ConditionDependenciesAvailable)
			rr := &types.ReconciliationRequest{
				Client:     cli,
				Instance:   instance,
				Conditions: condManager,
			}

			action := dependency.NewAction(dependency.MonitorOperator(config))
			err := action(ctx, rr)
			g.Expect(err).NotTo(HaveOccurred())

			gotCond := condManager.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(gotCond).NotTo(BeNil())

			if tt.expectedAvailable {
				g.Expect(gotCond.Status).To(Equal(metav1.ConditionTrue))
			} else {
				g.Expect(gotCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(gotCond.Reason).To(Equal("DependencyDegraded"))
				for _, substr := range tt.expectedMsgContains {
					g.Expect(gotCond.Message).To(ContainSubstring(substr))
				}
				for _, substr := range tt.expectedMsgNotContains {
					g.Expect(gotCond.Message).NotTo(ContainSubstring(substr))
				}
				g.Expect(gotCond.Severity).To(Equal(tt.severity))
			}
		})
	}
}

func TestDependencyAction_MultipleOperators(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	ctx := context.Background()
	cli := envTest.Client()
	nsn := xid.New().String()

	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsn,
		},
	}
	g.Expect(cli.Create(ctx, &ns)).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = cli.Delete(ctx, &ns) })

	createTestOperatorCRD(t, g, ctx, envTest)

	operator1 := createOperatorCR(xid.New().String(), nsn, testOperatorGVK)
	setCondition(g, operator1, "Ready", "True", "Ready", "Ready")
	createAndUpdateStatus(ctx, g, cli, operator1)
	t.Cleanup(func() { _ = cli.Delete(ctx, operator1) })

	operator2 := createOperatorCR(xid.New().String(), nsn, testOperatorGVK)
	setCondition(g, operator2, "Degraded", "True", "Failed", "Something went wrong")
	createAndUpdateStatus(ctx, g, cli, operator2)
	t.Cleanup(func() { _ = cli.Delete(ctx, operator2) })

	instance := &componentApi.Kueue{
		ObjectMeta: metav1.ObjectMeta{
			Name: xid.New().String(),
		},
	}

	condManager := cond.NewManager(instance, status.ConditionDependenciesAvailable)
	rr := &types.ReconciliationRequest{
		Client:     cli,
		Instance:   instance,
		Conditions: condManager,
	}

	action := dependency.NewAction(
		dependency.MonitorOperator(dependency.OperatorConfig{
			OperatorGVK: testOperatorGVK,
			CRName:      operator1.GetName(),
			CRNamespace: nsn,
		}),
		dependency.MonitorOperator(dependency.OperatorConfig{
			OperatorGVK: testOperatorGVK,
			CRName:      operator2.GetName(),
			CRNamespace: nsn,
		}),
	)

	err = action(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())

	gotCond := condManager.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(gotCond).NotTo(BeNil())
	g.Expect(gotCond.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(gotCond.Reason).To(Equal("DependencyDegraded"))
	g.Expect(gotCond.Message).To(ContainSubstring(testOperatorGVK.Kind))
	g.Expect(gotCond.Message).To(ContainSubstring("Degraded=True"))
}

// TestDependencyAction_VariableCRName verifies that getFirstCR correctly discovers operator CRs
// when CRName is not specified, for both namespace-scoped and cluster-scoped resources.
func TestDependencyAction_VariableCRName(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()
	nsn := xid.New().String()

	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsn}}
	g.Expect(cli.Create(ctx, &ns)).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = cli.Delete(ctx, &ns) })

	// Register namespace-scoped CRD (testOperatorGVK) and cluster-scoped CRD.
	createTestOperatorCRD(t, g, ctx, envTest)
	createTestClusterOperatorCRD(t, g, ctx, envTest)

	t.Run("namespace-scoped CR discovered without CRName", func(t *testing.T) {
		g := NewWithT(t)

		operatorCR := createOperatorCR(xid.New().String(), nsn, testOperatorGVK)
		setCondition(g, operatorCR, "Degraded", "True", "TestReason", "Test message")
		createAndUpdateStatus(ctx, g, cli, operatorCR)
		t.Cleanup(func() { _ = cli.Delete(ctx, operatorCR) })

		instance := &componentApi.Kueue{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
		condManager := cond.NewManager(instance, status.ConditionDependenciesAvailable)
		rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

		action := dependency.NewAction(dependency.MonitorOperator(dependency.OperatorConfig{
			OperatorGVK: testOperatorGVK,
			CRNamespace: nsn,
		}))

		err := action(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())

		gotCond := condManager.GetCondition(status.ConditionDependenciesAvailable)
		g.Expect(gotCond).NotTo(BeNil())
		g.Expect(gotCond.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(gotCond.Reason).To(Equal("DependencyDegraded"))
		g.Expect(gotCond.Message).To(ContainSubstring(testOperatorGVK.Kind))
		g.Expect(gotCond.Message).To(ContainSubstring("Degraded=True"))
		g.Expect(gotCond.Message).To(ContainSubstring("TestReason"))
	})

	t.Run("cluster-scoped CR discovered without CRName", func(t *testing.T) {
		g := NewWithT(t)

		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(testClusterOperatorGVK)
		cr.SetName(xid.New().String())
		setCondition(g, cr, "Degraded", "True", "ClusterFailed", "Cluster-scoped failure")
		createAndUpdateStatus(ctx, g, cli, cr)
		t.Cleanup(func() { _ = cli.Delete(ctx, cr) })

		instance := &componentApi.Kueue{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
		condManager := cond.NewManager(instance, status.ConditionDependenciesAvailable)
		rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

		// CRNamespace is intentionally empty — this exercises the cluster-scoped list path.
		action := dependency.NewAction(dependency.MonitorOperator(dependency.OperatorConfig{
			OperatorGVK: testClusterOperatorGVK,
		}))

		err := action(ctx, rr)
		g.Expect(err).NotTo(HaveOccurred())

		gotCond := condManager.GetCondition(status.ConditionDependenciesAvailable)
		g.Expect(gotCond).NotTo(BeNil())
		g.Expect(gotCond.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(gotCond.Reason).To(Equal("DependencyDegraded"))
		g.Expect(gotCond.Message).To(ContainSubstring(testClusterOperatorGVK.Kind))
		g.Expect(gotCond.Message).To(ContainSubstring("Degraded=True"))
		g.Expect(gotCond.Message).To(ContainSubstring("ClusterFailed"))
	})
}

// TestDependencyAction_MissingCRD verifies that DependenciesAvailable is True
// when the external operator's CRD is not installed.
func TestDependencyAction_MissingCRD(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

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

			instance := &componentApi.Kueue{
				ObjectMeta: metav1.ObjectMeta{
					Name: xid.New().String(),
				},
			}

			condManager := cond.NewManager(instance, status.ConditionDependenciesAvailable)
			rr := &types.ReconciliationRequest{
				Client:     cli,
				Instance:   instance,
				Conditions: condManager,
			}

			action := dependency.NewAction(
				dependency.MonitorOperator(dependency.OperatorConfig{
					OperatorGVK: nonExistentGVK,
					CRName:      tt.crName,
					CRNamespace: "any-namespace",
				}),
			)

			err := action(ctx, rr)
			g.Expect(err).NotTo(HaveOccurred())

			gotCond := condManager.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(gotCond).NotTo(BeNil())
			g.Expect(gotCond.Status).To(Equal(metav1.ConditionTrue),
				"DependenciesAvailable should be True when CRD doesn't exist")
		})
	}
}

// TestMonitorCRD verifies that MonitorCRD correctly reports CRD presence and respects Severity.
//
// Each subtest uses its own envtest instance rather than sharing one across subtests.
// HasCRD relies on the REST mapper, whose discovery cache refreshes asynchronously after
// CRD deletion. A shared instance cannot guarantee the mapper reflects zero CRDs at the
// start of the "absent CRD" case when other subtests registered CRDs beforehand.
func TestMonitorCRD(t *testing.T) {
	testCRDGVK := schema.GroupVersionKind{
		Group:   "test.opendatahub.io",
		Version: "v1",
		Kind:    "TestResource",
	}

	tests := []struct {
		name              string
		registerCRD       bool
		severity          common.ConditionSeverity
		expectedAvailable bool
		expectedSeverity  common.ConditionSeverity
	}{
		{
			name:              "absent CRD yields degraded with error severity by default",
			registerCRD:       false,
			expectedAvailable: false,
			expectedSeverity:  common.ConditionSeverityError,
		},
		{
			name:              "absent CRD with info severity yields degraded but not blocking",
			registerCRD:       false,
			severity:          common.ConditionSeverityInfo,
			expectedAvailable: false,
			expectedSeverity:  common.ConditionSeverityInfo,
		},
		{
			name:              "present CRD yields healthy",
			registerCRD:       true,
			severity:          common.ConditionSeverityError,
			expectedAvailable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			envTest, err := envt.New()
			g.Expect(err).NotTo(HaveOccurred())
			t.Cleanup(func() { _ = envTest.Stop() })

			ctx := context.Background()
			cli := envTest.Client()

			if tt.registerCRD {
				crd, err := envTest.RegisterCRD(ctx, testCRDGVK, "testresources", "testresource", apiextensionsv1.NamespaceScoped)
				g.Expect(err).NotTo(HaveOccurred())
				t.Cleanup(func() {
					g.Eventually(func() error {
						return cli.Delete(ctx, crd)
					}).Should(Or(Not(HaveOccurred()), MatchError(k8serr.IsNotFound, "IsNotFound")))
				})
			}

			instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
			condManager := cond.NewManager(instance, status.ConditionDependenciesAvailable)
			rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

			action := dependency.NewAction(dependency.MonitorCRD(dependency.CRDConfig{
				GVK:      testCRDGVK,
				Severity: tt.severity,
			}))
			err = action(ctx, rr)
			g.Expect(err).NotTo(HaveOccurred())

			gotCond := condManager.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(gotCond).NotTo(BeNil())

			if tt.expectedAvailable {
				g.Expect(gotCond.Status).To(Equal(metav1.ConditionTrue))
			} else {
				g.Expect(gotCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(gotCond.Message).To(ContainSubstring(testCRDGVK.Kind))
				g.Expect(gotCond.Severity).To(Equal(tt.expectedSeverity))
			}
		})
	}
}

// Helper functions

func createTestOperatorCRD(t *testing.T, g *WithT, ctx context.Context, envTest *envt.EnvT) {
	t.Helper()
	crd, err := envTest.RegisterCRD(ctx, testOperatorGVK, "testoperators", "testoperator", apiextensionsv1.NamespaceScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, envTest.Client(), crd)
}

func createTestClusterOperatorCRD(t *testing.T, g *WithT, ctx context.Context, envTest *envt.EnvT) {
	t.Helper()
	crd, err := envTest.RegisterCRD(ctx, testClusterOperatorGVK, "testclusteroperators", "testclusteroperator", apiextensionsv1.ClusterScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, envTest.Client(), crd)
}

func createOperatorCR(name, namespace string, gvk schema.GroupVersionKind) *unstructured.Unstructured {
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(gvk)
	cr.SetName(name)
	cr.SetNamespace(namespace)
	return cr
}

func setCondition(g *WithT, cr *unstructured.Unstructured, cType, status, reason, message string) {
	condition := metav1.Condition{
		Type:               cType,
		Status:             metav1.ConditionStatus(status),
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
	err := testf.SetTypedConditions(cr, []metav1.Condition{condition})
	g.Expect(err).NotTo(HaveOccurred())
}

func setMultipleConditions(g *WithT, cr *unstructured.Unstructured, conditions []metav1.Condition) {
	err := testf.SetTypedConditions(cr, conditions)
	g.Expect(err).NotTo(HaveOccurred())
}

func createAndUpdateStatus(ctx context.Context, g *WithT, cli client.Client, cr *unstructured.Unstructured) {
	// Store the status before creation (it will be stripped)
	statusObj, hasStatus, err := unstructured.NestedMap(cr.Object, "status")
	g.Expect(err).NotTo(HaveOccurred())

	// Create the CR (status is ignored)
	err = cli.Create(ctx, cr)
	g.Expect(err).NotTo(HaveOccurred())

	if hasStatus {
		// Re-apply the status and update it
		err = unstructured.SetNestedMap(cr.Object, statusObj, "status")
		g.Expect(err).NotTo(HaveOccurred())
		err = cli.Status().Update(ctx, cr)
		g.Expect(err).NotTo(HaveOccurred())
	}
}

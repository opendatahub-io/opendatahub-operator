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

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
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
	createTestOperatorCRD(t, g, ctx, cli)

	tests := []struct {
		name                string
		setupOperatorCR     func(g *WithT, ns string) *unstructured.Unstructured
		filter              dependency.DegradedConditionFilterFunc
		expectedAvailable   bool
		expectedMsgContains []string
	}{
		{
			name: "Degraded=True",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				operatorCR := createOperatorCR(xid.New().String(), ns, testOperatorGVK)
				setCondition(g, operatorCR, "Degraded", "True", "TestFailed", "Test failure message")
				createAndUpdateStatus(ctx, g, cli, operatorCR)
				return operatorCR
			},
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
			expectedAvailable: true,
		},
		{
			name: "operator not found",
			setupOperatorCR: func(g *WithT, ns string) *unstructured.Unstructured {
				return nil
			},
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
			expectedAvailable:   false,
			expectedMsgContains: []string{"Dependencies degraded", "Degraded=True", "First error", "Available=False", "Second error"},
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

			cond := condManager.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(cond).NotTo(BeNil())

			if tt.expectedAvailable {
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			} else {
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("DependencyDegraded"))
				for _, substr := range tt.expectedMsgContains {
					g.Expect(cond.Message).To(ContainSubstring(substr))
				}
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

	createTestOperatorCRD(t, g, ctx, cli)

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

	cond := condManager.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(cond).NotTo(BeNil())
	g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(cond.Reason).To(Equal("DependencyDegraded"))
	g.Expect(cond.Message).To(ContainSubstring(testOperatorGVK.Kind))
	g.Expect(cond.Message).To(ContainSubstring("Degraded=True"))
}

func TestDependencyAction_VariableCRName(t *testing.T) {
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

	createTestOperatorCRD(t, g, ctx, cli)

	operatorCR := createOperatorCR(xid.New().String(), nsn, testOperatorGVK)
	setCondition(g, operatorCR, "Degraded", "True", "TestReason", "Test message")
	createAndUpdateStatus(ctx, g, cli, operatorCR)
	t.Cleanup(func() { _ = cli.Delete(ctx, operatorCR) })

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
			CRNamespace: nsn,
		}),
	)

	err = action(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())

	cond := condManager.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(cond).NotTo(BeNil())
	g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
}

// Helper functions

func createTestOperatorCRD(t *testing.T, g *WithT, ctx context.Context, cli client.Client) {
	t.Helper()

	crd := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testoperators." + testOperatorGVK.Group,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: testOperatorGVK.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     testOperatorGVK.Kind,
				ListKind: testOperatorGVK.Kind + "List",
				Plural:   "testoperators",
				Singular: "testoperator",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    testOperatorGVK.Version,
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type:                   "object",
									XPreserveUnknownFields: pointer(true),
								},
								"status": {
									Type:                   "object",
									XPreserveUnknownFields: pointer(true),
								},
							},
						},
					},
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
				},
			},
		},
	}

	t.Cleanup(func() {
		g.Eventually(func() error {
			return cli.Delete(ctx, &crd)
		}).Should(Or(
			Not(HaveOccurred()),
			MatchError(k8serr.IsNotFound, "IsNotFound"),
		))
	})

	err := cli.Create(ctx, &crd)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		g.Expect(err).NotTo(HaveOccurred())
	}

	// Wait for CRD to be established
	g.Eventually(func() bool {
		err := cli.Get(ctx, client.ObjectKeyFromObject(&crd), &crd)
		if err != nil {
			return false
		}
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return true
			}
		}
		return false
	}).Should(BeTrue(), "CRD should be established")
}

func pointer[T any](v T) *T {
	return &v
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
	statusObj, hasStatus, _ := unstructured.NestedMap(cr.Object, "status")

	// Create the CR (status is ignored)
	err := cli.Create(ctx, cr)
	g.Expect(err).NotTo(HaveOccurred())

	if hasStatus {
		// Re-apply the status and update it
		_ = unstructured.SetNestedMap(cr.Object, statusObj, "status")
		err = cli.Status().Update(ctx, cr)
		g.Expect(err).NotTo(HaveOccurred())
	}
}

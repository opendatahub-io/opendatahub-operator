//nolint:testpackage
package setup

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestMarkUninstallInProgress(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsc"},
		Status: dscv2.DataScienceClusterStatus{
			Status: common.Status{
				Phase: status.PhaseReady,
				Conditions: []common.Condition{{
					Type:    status.ConditionTypeReady,
					Status:  metav1.ConditionTrue,
					Reason:  status.ReadyReason,
					Message: "DataScienceCluster is ready",
				}},
			},
		},
	}
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
		Status: dsciv2.DSCInitializationStatus{
			Phase: status.PhaseReady,
			Conditions: []common.Condition{
				{
					Type:    status.ConditionTypeReady,
					Status:  metav1.ConditionTrue,
					Reason:  status.ReadyReason,
					Message: "DSCInitialization is ready",
				},
				{
					Type:    status.ConditionTypeAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  status.ReconcileCompleted,
					Message: status.ReconcileCompletedMessage,
				},
			},
		},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsc, dsci),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, c client.Client, _ string, obj client.Object, _ ...client.SubResourceUpdateOption) error {
				return c.Update(ctx, obj)
			},
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())

	r := &SetupControllerReconciler{Client: cli}
	g.Expect(r.markUninstallInProgress(ctx)).To(Succeed())

	updatedDSC := &dscv2.DataScienceCluster{}
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dsc), updatedDSC)).To(Succeed())
	g.Expect(updatedDSC.Status.Phase).To(Equal(status.PhaseNotReady))
	assertUninstallCondition(g, updatedDSC.Status.Conditions, status.ConditionTypeReady, metav1.ConditionFalse)

	updatedDSCI := &dsciv2.DSCInitialization{}
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(dsci), updatedDSCI)).To(Succeed())
	g.Expect(updatedDSCI.Status.Phase).To(Equal(status.PhaseNotReady))
	assertUninstallCondition(g, updatedDSCI.Status.Conditions, status.ConditionTypeReady, metav1.ConditionFalse)
	assertUninstallCondition(g, updatedDSCI.Status.Conditions, status.ConditionTypeAvailable, metav1.ConditionFalse)
	assertUninstallCondition(g, updatedDSCI.Status.Conditions, status.ConditionTypeProgressing, metav1.ConditionTrue)
}

func assertUninstallCondition(g Gomega, conditions []common.Condition, conditionType string, conditionStatus metav1.ConditionStatus) {
	for i := range conditions {
		condition := &conditions[i]
		if condition.Type != conditionType {
			continue
		}

		g.Expect(condition.Status).To(Equal(conditionStatus))
		g.Expect(condition.Reason).To(Equal(status.UninstallInProgressReason))
		g.Expect(condition.Message).To(Equal(status.UninstallInProgressMessage))
		return
	}

	g.Expect(conditions).To(ContainElement(HaveField("Type", conditionType)))
}

func TestFilterDeleteConfigMapPredicateWithTypedAndUnstructured(t *testing.T) {
	const operatorNs = "test-operator-ns"

	r := &SetupControllerReconciler{}
	preds := r.filterDeleteConfigMap(operatorNs)

	tests := []struct {
		name string
		obj  client.Object
		want bool
	}{
		{
			name: "typed ConfigMap with correct namespace and label",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-cm",
					Namespace: operatorNs,
					Labels:    map[string]string{upgrade.DeleteConfigMapLabel: "true"},
				},
			},
			want: true,
		},
		{
			name: "typed ConfigMap wrong namespace",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-cm",
					Namespace: "other-ns",
					Labels:    map[string]string{upgrade.DeleteConfigMapLabel: "true"},
				},
			},
			want: false,
		},
		{
			name: "typed ConfigMap missing label",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-cm",
					Namespace: operatorNs,
				},
			},
			want: false,
		},
		{
			name: "unstructured ConfigMap with correct namespace and label",
			obj: func() client.Object {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
				u.SetName("delete-cm")
				u.SetNamespace(operatorNs)
				u.SetLabels(map[string]string{upgrade.DeleteConfigMapLabel: "true"})
				return u
			}(),
			want: true,
		},
		{
			name: "unstructured ConfigMap wrong namespace",
			obj: func() client.Object {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
				u.SetName("delete-cm")
				u.SetNamespace("other-ns")
				u.SetLabels(map[string]string{upgrade.DeleteConfigMapLabel: "true"})
				return u
			}(),
			want: false,
		},
		{
			name: "unstructured ConfigMap missing label",
			obj: func() client.Object {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
				u.SetName("delete-cm")
				u.SetNamespace(operatorNs)
				return u
			}(),
			want: false,
		},
		{
			name: "unstructured non-ConfigMap with correct namespace and label",
			obj: func() client.Object {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
				u.SetName("delete-cm")
				u.SetNamespace(operatorNs)
				u.SetLabels(map[string]string{upgrade.DeleteConfigMapLabel: "true"})
				return u
			}(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(preds.Create(event.CreateEvent{Object: tt.obj})).
				To(Equal(tt.want), "CreateFunc")

			g.Expect(preds.Update(event.UpdateEvent{ObjectNew: tt.obj})).
				To(Equal(tt.want), "UpdateFunc")
		})
	}
}

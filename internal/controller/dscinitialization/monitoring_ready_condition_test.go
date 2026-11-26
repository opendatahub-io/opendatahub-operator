//nolint:testpackage // Exercises the package-private GetMonitoringReadyCondition directly.
package dscinitialization

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func dsciWithMonitoring(state operatorv1.ManagementState) *dsciv2.DSCInitialization {
	dsci := &dsciv2.DSCInitialization{}
	dsci.Spec.Monitoring.ManagementState = state
	return dsci
}

func establishedCertManagerCRD(name string) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group:    "cert-manager.io",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1", Served: true}},
		},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{{
				Type:   apiextensionsv1.Established,
				Status: apiextensionsv1.ConditionTrue,
			}},
		},
	}
}

func certManagerCRDs() []client.Object {
	return []client.Object{
		establishedCertManagerCRD(gvk.CertManagerIssuerCRDName),
		establishedCertManagerCRD(gvk.CertManagerCertificateCRDName),
	}
}

func monitoringCR(conditions ...map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk.Monitoring)
	u.SetName(serviceApi.MonitoringInstanceName)
	if len(conditions) > 0 {
		raw := make([]any, 0, len(conditions))
		for _, c := range conditions {
			raw = append(raw, c)
		}
		_ = unstructured.SetNestedSlice(u.Object, raw, "status", "conditions")
	}
	return u
}

func cond(condType, condStatus string) map[string]any {
	return map[string]any{
		"type":    condType,
		"status":  condStatus,
		"reason":  "TestReason",
		"message": "test message",
	}
}

func findCond(conds []DSCInitializationCondition, condType string) *DSCInitializationCondition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

// TestGetMonitoringReadyCondition_CertManagerMissing verifies the clean-cluster
// scenario: monitoring is enabled via the DSCI spec but cert-manager is not
// installed (so neither the Monitoring CRD nor its CR exist). MonitoringReady is
// reported False/DependenciesNotReady rather than the misleading "not enabled".
func TestGetMonitoringReadyCondition_CertManagerMissing(t *testing.T) {
	g := NewWithT(t)

	// No Monitoring CR, no cert-manager CRDs; monitoring Managed in the spec.
	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())

	r := &DSCInitializationReconciler{Client: cli}
	conds := r.GetMonitoringReadyCondition(t.Context(), dsciWithMonitoring(operatorv1.Managed))

	ready := findCond(conds, status.ConditionMonitoringReady)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.ReadyStatus).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.ReadyReason).To(Equal(status.DependenciesNotReadyReason))
	g.Expect(ready.ReadyMessage).To(ContainSubstring("cert-manager"))
}

// TestGetMonitoringReadyCondition_CertManagerPresentNoConditions verifies that
// with cert-manager installed but the operator not yet reporting any conditions,
// the existing "initializing" (Unknown) behavior is preserved.
func TestGetMonitoringReadyCondition_CertManagerPresentNoConditions(t *testing.T) {
	g := NewWithT(t)

	objs := append([]client.Object{monitoringCR()}, certManagerCRDs()...)
	cli, err := fakeclient.New(fakeclient.WithObjects(objs...))
	g.Expect(err).NotTo(HaveOccurred())

	r := &DSCInitializationReconciler{Client: cli}
	conds := r.GetMonitoringReadyCondition(t.Context(), dsciWithMonitoring(operatorv1.Managed))

	g.Expect(conds).To(HaveLen(1))
	g.Expect(conds[0].Type).To(Equal(status.ConditionMonitoringReady))
	g.Expect(conds[0].ReadyStatus).To(Equal(metav1.ConditionUnknown))
}

// TestGetMonitoringReadyCondition_HonorsFalseMirroredCondition verifies the
// aggregate MonitoringReady reflects a False whitelisted condition reported by
// the operator, instead of unconditionally marking Ready=True.
func TestGetMonitoringReadyCondition_HonorsFalseMirroredCondition(t *testing.T) {
	g := NewWithT(t)

	monitoring := monitoringCR(
		cond(status.ConditionTypeProvisioningSucceeded, string(metav1.ConditionTrue)),
		cond(status.ConditionMonitoringStackAvailable, string(metav1.ConditionFalse)),
	)
	objs := append([]client.Object{monitoring}, certManagerCRDs()...)
	cli, err := fakeclient.New(fakeclient.WithObjects(objs...))
	g.Expect(err).NotTo(HaveOccurred())

	r := &DSCInitializationReconciler{Client: cli}
	conds := r.GetMonitoringReadyCondition(t.Context(), dsciWithMonitoring(operatorv1.Managed))

	ready := findCond(conds, status.ConditionMonitoringReady)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.ReadyStatus).To(Equal(metav1.ConditionFalse))
}

// TestGetMonitoringReadyCondition_AllReady verifies the healthy path still
// produces MonitoringReady=True when the operator reports its conditions True.
func TestGetMonitoringReadyCondition_AllReady(t *testing.T) {
	g := NewWithT(t)

	monitoring := monitoringCR(
		cond(status.ConditionTypeReady, string(metav1.ConditionTrue)),
		cond(status.ConditionMonitoringStackAvailable, string(metav1.ConditionTrue)),
	)
	objs := append([]client.Object{monitoring}, certManagerCRDs()...)
	cli, err := fakeclient.New(fakeclient.WithObjects(objs...))
	g.Expect(err).NotTo(HaveOccurred())

	r := &DSCInitializationReconciler{Client: cli}
	conds := r.GetMonitoringReadyCondition(t.Context(), dsciWithMonitoring(operatorv1.Managed))

	ready := findCond(conds, status.ConditionMonitoringReady)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.ReadyStatus).To(Equal(metav1.ConditionTrue))
	g.Expect(ready.ReadyReason).To(Equal(status.ReadyReason))
}

// TestGetMonitoringReadyCondition_MonitoringDisabled verifies that when
// monitoring is Removed in the DSCI spec, the cert-manager check is skipped
// (cert-manager is not required) and MonitoringReady=False/Removed is reported.
func TestGetMonitoringReadyCondition_MonitoringDisabled(t *testing.T) {
	g := NewWithT(t)

	// No Monitoring CR, no cert-manager CRDs; monitoring Removed in the spec.
	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())

	r := &DSCInitializationReconciler{Client: cli}
	conds := r.GetMonitoringReadyCondition(t.Context(), dsciWithMonitoring(operatorv1.Removed))

	g.Expect(conds).To(HaveLen(1))
	g.Expect(conds[0].Type).To(Equal(status.ConditionMonitoringReady))
	g.Expect(conds[0].ReadyStatus).To(Equal(metav1.ConditionFalse))
	g.Expect(conds[0].ReadyReason).To(Equal(status.RemovedReason))
}

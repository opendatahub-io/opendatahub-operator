package v3_test

import (
	"encoding/json"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	v3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v3"

	. "github.com/onsi/gomega"
)

func TestMaaSProvenanceAdmission(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*dscv3.DataScienceCluster)
		preserved bool
	}{
		{name: "unchanged", change: func(*dscv3.DataScienceCluster) {}, preserved: true},
		{name: "metadata", change: func(dsc *dscv3.DataScienceCluster) { dsc.Labels = map[string]string{"test": "kept"} }, preserved: true},
		{name: "unrelated spec", change: func(dsc *dscv3.DataScienceCluster) {
			dsc.Spec.Components.Dashboard.Standard.ManagementState = operatorv1.Managed
		}, preserved: true},
		{name: "forged marker", change: func(dsc *dscv3.DataScienceCluster) { dsc.Annotations[dscv3.MaaSV2StateAnnotation] = "forged" }, preserved: true},
		{name: "omitted marker", change: func(dsc *dscv3.DataScienceCluster) { delete(dsc.Annotations, dscv3.MaaSV2StateAnnotation) }, preserved: true},
		{name: "canonical enablement", change: func(dsc *dscv3.DataScienceCluster) {
			dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
		}},
		{name: "KServe parent", change: func(dsc *dscv3.DataScienceCluster) { dsc.Spec.Components.Kserve.ManagementState = operatorv1.Removed }, preserved: true},
		{name: "AI Gateway parent", change: func(dsc *dscv3.DataScienceCluster) {
			dsc.Spec.Components.AIGateway.ManagementState = operatorv1.Removed
		}, preserved: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			old := &dscv3.DataScienceCluster{}
			old.Spec.Components.Kserve.ManagementState = operatorv1.Managed
			old.Spec.Components.AIGateway.ManagementState = operatorv1.Managed
			old.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
			old.Annotations = map[string]string{"unrelated": "kept"}
			old.PreserveMaaSV2State(operatorv1.Removed, operatorv1.Managed)
			payload, err := json.Marshal(old)
			g.Expect(err).NotTo(HaveOccurred())
			current := old.DeepCopy()
			tc.change(current)
			ctx := admission.NewContextWithRequest(t.Context(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Update, OldObject: runtime.RawExtension{Raw: payload},
			}})
			defaulter := &v3webhook.Defaulter{Name: "test-v3"}
			g.Expect(defaulter.Default(ctx, current)).To(Succeed())
			g.Expect(current.ReadMaaSV2State()).To(Equal(tc.preserved))
			g.Expect(current.Annotations).To(HaveKeyWithValue("unrelated", "kept"))
			g.Expect(old.ReadMaaSV2State()).To(BeTrue(), "admission must not mutate the old object")
			if !tc.preserved {
				// Returning to the original values and submitting a stale marker cannot resurrect history.
				retired := current.DeepCopy()
				current = old.DeepCopy()
				g.Expect(current.AdmitMaaSV2State(retired)).To(Succeed())
				g.Expect(current.ReadMaaSV2State()).To(BeFalse())
			}
		})
	}
}

func TestMaaSProvenanceCreateAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		operation admissionv1.Operation
		old       string
		errorText string
	}{
		{name: "native create", operation: admissionv1.Create},
		{name: "missing old object", operation: admissionv1.Update, errorText: "decode old DSC"},
		{name: "malformed old object", operation: admissionv1.Update, old: "{", errorText: "decode old DSC"},
		{name: "invalid old marker", operation: admissionv1.Update,
			old:       `{"metadata":{"annotations":{"conversion.opendatahub.io/maas-v2-state":"unknown"}}}`,
			errorText: "invalid MaaS v2 provenance marker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			current := &dscv3.DataScienceCluster{}
			current.PreserveMaaSV2State(operatorv1.Removed, operatorv1.Managed)
			ctx := admission.NewContextWithRequest(t.Context(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: tc.operation, OldObject: runtime.RawExtension{Raw: []byte(tc.old)},
			}})
			defaulter := &v3webhook.Defaulter{Name: "test-v3"}
			err := defaulter.Default(ctx, current)
			if tc.errorText != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tc.errorText)))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(current.ReadMaaSV2State()).To(BeFalse())
			}
		})
	}
}

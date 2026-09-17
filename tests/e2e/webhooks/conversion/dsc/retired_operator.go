package dsc

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega" //nolint:staticcheck // Matchers use Gomega's test DSL.
)

// These are fresh-install retirement cases. They do not depend on the old
// TrainingOperator or LlamaStackOperator module being bundled in the image.
func (m WebhookSuite) runRetiredOperator(t *testing.T, field string) {
	t.Helper()

	for _, testCase := range []struct {
		name    string
		present bool
		state   operatorv1.ManagementState
	}{
		{name: "explicit_removed", present: true, state: operatorv1.Removed},
		{name: "empty", present: true},
		{name: "omitted"},
	} {
		t.Run("v2_v3/"+testCase.name, func(t *testing.T) {
			w := m.newScenario(t)
			g := NewWithT(t)
			object := retiredOperatorDSC(dscv2.GroupVersion.String(), field, testCase.present, testCase.state)
			g.Expect(w.Client().Create(t.Context(), object)).To(Succeed())
			assertFieldsAbsentFromV3(t, w, field)
			assertRetiredV2State(t, w, field)
		})
	}

	t.Run("v3_v2", func(t *testing.T) {
		w := m.newScenario(t)
		g := NewWithT(t)
		g.Expect(w.Client().Create(t.Context(), retiredOperatorDSC(dscv3.GroupVersion.String(), field, false, ""))).To(Succeed())
		assertFieldsAbsentFromV3(t, w, field)
		assertRetiredV2State(t, w, field)
	})

	t.Run("reject_managed_create", func(t *testing.T) {
		w := m.newScenario(t)
		g := NewWithT(t)
		err := w.Client().Create(t.Context(), retiredOperatorDSC(dscv2.GroupVersion.String(), field, true, operatorv1.Managed))
		g.Expect(k8serr.IsInvalid(err)).To(BeTrue(), "a new v2 %s=Managed request must be rejected: %v", field, err)
	})

	t.Run("reject_reenable_but_allow_unrelated_update", func(t *testing.T) {
		w := m.newScenario(t)
		g := NewWithT(t)
		object := retiredOperatorDSC(dscv2.GroupVersion.String(), field, true, operatorv1.Removed)
		g.Expect(w.Client().Create(t.Context(), object)).To(Succeed())
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest := retiredOperatorDSC(dscv2.GroupVersion.String(), field, false, "")
			if err := w.Client().Get(t.Context(), client.ObjectKeyFromObject(object), latest); err != nil {
				return err
			}
			if err := unstructured.SetNestedField(latest.Object, string(operatorv1.Managed), "spec", "components", field, "managementState"); err != nil {
				return err
			}
			return w.Client().Update(t.Context(), latest)
		})
		g.Expect(k8serr.IsInvalid(err)).To(BeTrue(), "v2 %s Removed-to-Managed must be rejected: %v", field, err)

		g.Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest := retiredOperatorDSC(dscv2.GroupVersion.String(), field, false, "")
			if err := w.Client().Get(t.Context(), client.ObjectKeyFromObject(object), latest); err != nil {
				return err
			}
			labels := latest.GetLabels()
			if labels == nil {
				labels = make(map[string]string)
			}
			labels["unrelated-update"] = "allowed"
			latest.SetLabels(labels)
			return w.Client().Update(t.Context(), latest)
		})).To(Succeed())
		g.Expect(w.Client().Get(t.Context(), client.ObjectKeyFromObject(object), object)).To(Succeed())
		g.Expect(object.GetLabels()).To(HaveKeyWithValue("unrelated-update", "allowed"))
		assertFieldsAbsentFromV3(t, w, field)
		assertRetiredV2State(t, w, field)
	})
}

func retiredOperatorDSC(apiVersion, field string, present bool, state operatorv1.ManagementState) *unstructured.Unstructured {
	components := map[string]any{}
	if present {
		component := map[string]any{}
		if state != "" {
			component["managementState"] = string(state)
		}
		components[field] = component
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       "DataScienceCluster",
		"metadata":   map[string]any{"name": webhookSuiteDSCName},
		"spec":       map[string]any{"components": components},
	}}
}

func assertRetiredV2State(t *testing.T, w *testf.WithT, field string) {
	t.Helper()
	g := NewWithT(t)
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(dscv2.GroupVersion.WithKind("DataScienceCluster"))
	g.Expect(w.Client().Get(t.Context(), client.ObjectKey{Name: webhookSuiteDSCName}, object)).To(Succeed())
	for _, root := range []string{"spec", "status"} {
		state, found, err := unstructured.NestedString(object.Object, root, "components", field, "managementState")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeTrue(), "v2 %s.components.%s.managementState must exist", root, field)
		g.Expect(state).To(Equal(string(operatorv1.Removed)))
	}
}

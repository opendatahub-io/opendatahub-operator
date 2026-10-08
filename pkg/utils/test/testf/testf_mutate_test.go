package testf_test

import (
	"errors"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func TestMutateConvertsAndAppliesTypedMutation(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(dscApi.AddToScheme(scheme)).To(Succeed())

	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": dscApi.GroupVersion.String(),
		"kind":       "DataScienceCluster",
		"metadata":   map[string]any{"name": "test-dsc"},
	}}

	err := testf.Mutate[*dscApi.DataScienceCluster](scheme, func(dsc *dscApi.DataScienceCluster) error {
		dsc.Spec.Components.AIHub.ManagementState = operatorv1.Managed
		return nil
	})(obj)
	g.Expect(err).NotTo(HaveOccurred())

	state, found, err := unstructured.NestedString(obj.Object, "spec", "components", "aiHub", "managementState")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(state).To(Equal(string(operatorv1.Managed)))
}

func TestMutateReturnsMutationError(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(dscApi.AddToScheme(scheme)).To(Succeed())

	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": dscApi.GroupVersion.String(),
		"kind":       "DataScienceCluster",
	}}
	wantErr := errors.New("mutation failed")

	err := testf.Mutate[*dscApi.DataScienceCluster](scheme, func(*dscApi.DataScienceCluster) error {
		return wantErr
	})(obj)
	g.Expect(err).To(MatchError(wantErr))
}

func TestMutateReturnsConversionError(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(dscApi.AddToScheme(scheme)).To(Succeed())

	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": dscApi.GroupVersion.String(),
		"kind":       "DataScienceCluster",
		"metadata":   map[string]any{"labels": []any{"invalid-label-map"}},
	}}

	err := testf.Mutate[*dscApi.DataScienceCluster](scheme, func(*dscApi.DataScienceCluster) error {
		return nil
	})(obj)
	g.Expect(err).To(MatchError(ContainSubstring("convert datasciencecluster.opendatahub.io/v3, Kind=DataScienceCluster to typed object")))
}

func TestMutateRejectsTypeMismatch(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(dscApi.AddToScheme(scheme)).To(Succeed())

	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": dscApi.GroupVersion.String(),
		"kind":       "DataScienceCluster",
	}}

	err := testf.Mutate[*unstructured.Unstructured](scheme, func(*unstructured.Unstructured) error {
		return nil
	})(obj)
	g.Expect(err).To(MatchError(ContainSubstring("not requested type")))
}

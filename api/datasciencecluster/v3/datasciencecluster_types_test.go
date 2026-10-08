package v3

import (
	"testing"

	. "github.com/onsi/gomega"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestRegistrationAndDeepCopy(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(AddToScheme(scheme)).To(Succeed())

	object, err := scheme.New(GroupVersion.WithKind("DataScienceCluster"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(object).To(BeAssignableToTypeOf(&DataScienceCluster{}))

	source := &DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "default-dsc",
			Labels: map[string]string{"source": "v3"},
		},
		Spec: DataScienceClusterSpec{Components: Components{}},
	}
	source.Spec.Components.Dashboard.Standard.ManagementState = operatorv1.Managed

	copy := source.DeepCopy()
	copy.Labels["source"] = "changed"
	copy.Spec.Components.Dashboard.Standard.ManagementState = operatorv1.Removed
	g.Expect(source.Labels["source"]).To(Equal("v3"))
	g.Expect(source.Spec.Components.Dashboard.Standard.ManagementState).To(Equal(operatorv1.Managed))

	list, err := scheme.New(GroupVersion.WithKind("DataScienceClusterList"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(list).To(BeAssignableToTypeOf(&DataScienceClusterList{}))

	sourceList := &DataScienceClusterList{Items: []DataScienceCluster{*source.DeepCopy()}}
	listCopy := sourceList.DeepCopy()
	listCopy.Items[0].Labels["source"] = "list-copy"
	g.Expect(sourceList.Items[0].Labels["source"]).To(Equal("v3"))
}

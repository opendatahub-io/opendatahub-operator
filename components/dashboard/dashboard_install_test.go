package dashboard_test

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Checking if dashboard is installed", func() {

	d := &dashboard.Dashboard{}

	It("should indicate it is not installed when DSC status is true", func() {
		// given
		dsc := dscWithStatusOf("dashboard", true)

		// when
		installed, err := d.IsInstalled(dsc)

		// then
		Expect(err).ToNot(HaveOccurred())
		Expect(installed).To(BeTrue())
	})

	It("should indicate it is not installed when DSC status is false", func() {
		// given
		dsc := dscWithStatusOf("dashboard", false)

		// when
		installed, err := d.IsInstalled(dsc)

		// then
		Expect(err).ToNot(HaveOccurred())
		Expect(installed).To(BeFalse())
	})

	It("should indicate it is not installed when is not reported as DSC status", func() {
		// given
		d := &dashboard.Dashboard{}
		dsc := dscWithStatusOf("kserve", true)

		// when
		installed, err := d.IsInstalled(dsc)

		// then
		Expect(err).ToNot(HaveOccurred())
		Expect(installed).To(BeFalse())
	})

})

func dscWithStatusOf(componentName string, isInstalled bool) *v1.DataScienceCluster {
	return &v1.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "DataScienceCluster",
		},
		Status: v1.DataScienceClusterStatus{
			InstalledComponents: map[string]bool{
				componentName: isInstalled,
			},
		},
	}
}

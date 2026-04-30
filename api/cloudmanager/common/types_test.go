package common_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
)

func TestLWSDependencyGetNamespace(t *testing.T) {
	t.Run("returns configured namespace", func(t *testing.T) {
		g := NewWithT(t)

		d := common.LWSDependency{
			Configuration: common.LWSConfiguration{
				Namespace: "custom-lws-ns",
			},
		}

		g.Expect(d.GetNamespace()).To(Equal("custom-lws-ns"))
	})

	t.Run("returns default when empty", func(t *testing.T) {
		g := NewWithT(t)

		d := common.LWSDependency{}

		g.Expect(d.GetNamespace()).To(Equal(common.DefaultNamespaceLWSOperator))
	})
}

func TestSailOperatorDependencyGetNamespace(t *testing.T) {
	t.Run("returns configured namespace", func(t *testing.T) {
		g := NewWithT(t)

		d := common.SailOperatorDependency{
			Configuration: common.SailOperatorConfiguration{
				Namespace: "custom-istio-ns",
			},
		}

		g.Expect(d.GetNamespace()).To(Equal("custom-istio-ns"))
	})

	t.Run("returns default when empty", func(t *testing.T) {
		g := NewWithT(t)

		d := common.SailOperatorDependency{}

		g.Expect(d.GetNamespace()).To(Equal(common.DefaultNamespaceSailOperator))
	})
}

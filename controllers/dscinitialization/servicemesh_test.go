package dscinitialization_test

import (
	"github.com/go-logr/logr"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/controllers/dscinitialization"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var defaultAudiences = []string{"https://kubernetes.default.svc"}

var _ = Describe("Handling Token Audiences", func() {
	Context("Determining if provided audiences are default", func() {
		When("no specific audiences are provided", func() {
			It("should consider the audiences to be default", func() {
				var specAudiences *[]string
				Expect(dsci.IsDefaultAudiences(specAudiences)).To(BeTrue())
			})
		})

		When("the default audiences are provided", func() {
			It("should consider these audiences to be default", func() {
				specAudiences := &defaultAudiences
				Expect(dsci.IsDefaultAudiences(specAudiences)).To(BeTrue())
			})
		})

		When("non-default audiences are provided", func() {
			It("should not consider these audiences to be default", func() {
				specAudiences := &[]string{"https://example.com"}
				Expect(dsci.IsDefaultAudiences(specAudiences)).To(BeFalse())
			})
		})
	})

	Context("Determining the effective cluster audiences", func() {
		When("non-default audiences are provided", func() {
			It("should return the provided audiences", func() {
				specAudiences := &[]string{"https://example.com"}
				Expect(dsci.GetEffectiveClusterAudiences(specAudiences, nil, logr.Logger{})).To(Equal(*specAudiences))
			})
		})
	})
})

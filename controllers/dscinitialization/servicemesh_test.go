package dscinitialization_test

import (
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/controllers/dscinitialization"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var defaultAudiences = []string{"https://kubernetes.default.svc"}

var _ = Describe("Audience", func() {
	Describe("IsDefaultAudiences", func() {
		It("should return true for nil slice", func() {
			var specAudiences *[]string
			Expect(dsci.IsDefaultAudiences(specAudiences)).To(BeTrue())
		})

		It("should return true for default audiences", func() {
			specAudiences := &defaultAudiences
			Expect(dsci.IsDefaultAudiences(specAudiences)).To(BeTrue())
		})

		It("should return false for non-default audiences", func() {
			specAudiences := &[]string{"https://example.com"}
			Expect(dsci.IsDefaultAudiences(specAudiences)).To(BeFalse())
		})
	})

	Describe("DefinedAudiencesOrDefault", func() {
		It("should return spec audiences for non-default spec audiences", func() {
			specAudiences := &[]string{"https://example.com"}
			Expect(dsci.DefinedAudiencesOrDefault(specAudiences)).To(Equal(*specAudiences))
		})
	})

})

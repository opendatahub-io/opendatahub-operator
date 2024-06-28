package provider_test

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/provider"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DataProvider", func() {
	var (
		ctx context.Context
		c   client.Client
	)

	BeforeEach(func() {
		ctx = context.TODO()
		// Initialize your client here
	})

	Context("Using DataProvider with default value", func() {

		It("should return the default value if the original value is zero", func() {
			var originalValue int
			defaultValue := 10
			dataProviderWithDefault := provider.ValueOf(originalValue).OrElse(defaultValue)

			data, err := dataProviderWithDefault(ctx, c)
			Expect(err).NotTo(HaveOccurred())
			Expect(data).To(Equal(defaultValue))
		})

		It("should return the default slice value if original zero", func() {
			var originalValue []int
			defaultValue := []int{1, 2, 3, 4}
			dataProviderWithDefault := provider.ValueOf(originalValue).OrElse(defaultValue)

			data, err := dataProviderWithDefault(ctx, c)
			Expect(err).NotTo(HaveOccurred())
			Expect(data).To(Equal(defaultValue))
		})
	})

	// TODO write also tests for defaulter

})

package provider_test

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/provider"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Using DataProvider with default value", func() {

	var fakeClient client.Client

	BeforeEach(func() {
		fakeClient = fake.NewClientBuilder().
			WithObjects(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
				},
			}).
			Build()
	})

	It("should return the default value if the original value is zero", func(ctx context.Context) {
		var originalValue int
		defaultValue := 10
		actualValue := provider.ValueOf(originalValue).OrElse(defaultValue)

		Expect(actualValue).To(Equal(defaultValue))
	})

	It("should return the default slice value if original zero", func(ctx context.Context) {
		var originalValue []int
		defaultValue := []int{1, 2, 3, 4}
		actualValue := provider.ValueOf(originalValue).OrElse(defaultValue)

		Expect(actualValue).To(Equal(defaultValue))
	})

	It("should fall back to Get function when passed value is not defined", func(ctx context.Context) {
		var nonExistingService *corev1.Service
		serviceProviderWithDefault := provider.ValueOf(nonExistingService).OrGet(func() (*corev1.Service, error) {
			service := &corev1.Service{}
			if errGet := fakeClient.Get(ctx, client.ObjectKey{Name: "test-service", Namespace: "test-namespace"}, service); errGet != nil {
				return nil, errGet
			}

			return service, nil
		})

		actualService, err := serviceProviderWithDefault.Get()

		Expect(err).NotTo(HaveOccurred())
		Expect(actualService.Name).To(Equal("test-service"))
	})

})

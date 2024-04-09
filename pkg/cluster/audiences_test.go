package cluster_test

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	authentication "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var defaultAudiences = []string{"https://kubernetes.default.svc"}

var _ = Describe("Handling Token Audiences", func() {
	var fakeClient client.Client
	var logger logr.Logger

	BeforeEach(func() {
		fakeClient = fake.NewClientBuilder().WithScheme(testScheme).Build()
		logger = logr.Logger{}
	})

	Context("Determining the effective cluster audiences", func() {
		When("non-default audiences are provided", func() {
			It("should return the provided audiences", func() {
				specAudiences := []string{"https://example.com"}
				Expect(cluster.GetEffectiveClusterAudiences(fakeClient, logger, &specAudiences, cluster.GetSAToken)).To(Equal(specAudiences))
			})
		})

		When("non-default audiences are fetched successfully", func() {
			It("should return the fetched audiences", func() {
				mockAudiences := []string{"https://mock.audience.com"}
				mockGetSAToken := func() (string, error) {
					return "mock_token", nil
				}

				baseClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
				fakeClient := &mockAudiencesClient{Client: baseClient, mockAudiences: mockAudiences}

				Expect(cluster.GetEffectiveClusterAudiences(fakeClient, logger, nil, mockGetSAToken)).To(Equal(mockAudiences))
			})
		})

		When("an error occurs while fetching the audiences", func() {
			It("should return the default audiences", func() {
				mockFailGettingToken := func() (string, error) {
					return "", fmt.Errorf("we failed getting token")
				}

				Expect(cluster.GetEffectiveClusterAudiences(fakeClient, logger, nil, mockFailGettingToken)).To(Equal(defaultAudiences))
			})
		})
	})
})

type mockAudiencesClient struct {
	client.Client
	mockAudiences []string
}

func (mac *mockAudiencesClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if tokenReview, isTokenReview := obj.(*authentication.TokenReview); isTokenReview {
		tokenReview.Status.Audiences = mac.mockAudiences
		tokenReview.Status.Authenticated = true
		return nil
	}
	return mac.Client.Create(ctx, obj, opts...)
}

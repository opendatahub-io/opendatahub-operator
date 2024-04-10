package cluster_test

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	authentication "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var defaultAudiences = []string{"https://kubernetes.default.svc"}

var _ = Describe("Handling Token Audiences", func() {
	var logger logr.Logger

	BeforeEach(func() {
		logger = logr.Logger{}
	})

	Context("Determining the effective cluster audiences", func() {
		When("non-default audiences are provided", func() {
			It("should return the provided audiences", func() {
				specAudiences := []string{"https://example.com"}
				Expect(cluster.GetEffectiveClusterAudiences(nil, logger, &specAudiences, cluster.GetSAToken)).To(Equal(specAudiences))
			})
		})

		When("non-default audiences are fetched successfully", func() {
			It("should return the fetched audiences", func() {
				stubAudiences := []string{"https://stub.audience.com"}
				stubGetSAToken := func() (string, error) {
					return "stub_token", nil
				}

				fakeClient := &stubAudiencesClient{stubAudiences: stubAudiences}

				Expect(cluster.GetEffectiveClusterAudiences(fakeClient, logger, nil, stubGetSAToken)).To(Equal(stubAudiences))
			})
		})

		When("an error occurs while fetching the audiences", func() {
			It("should return the default audiences", func() {
				stubFailGettingToken := func() (string, error) {
					return "", fmt.Errorf("we failed getting token")
				}

				Expect(cluster.GetEffectiveClusterAudiences(nil, logger, nil, stubFailGettingToken)).To(Equal(defaultAudiences))
			})
		})
	})
})

type stubAudiencesClient struct {
	client.Client
	stubAudiences []string
}

// controller-runtime fake client package does not allow to hook into request/response chain, unlike client-go
// fake clientset, where we could use "reactors" [1]
// To manipulate TokenReview.Status (from where the audiences are read) we need to hook
// into response of the Create operation, so stubbing client.Client#Create is the easiest and sufficient option.
//
// [1] https://pkg.go.dev/k8s.io/client-go@v0.29.3/testing#Fake.AddReactor
func (s *stubAudiencesClient) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	if tokenReview, isTokenReview := obj.(*authentication.TokenReview); isTokenReview {
		tokenReview.Status.Audiences = s.stubAudiences
		tokenReview.Status.Authenticated = true
		return nil
	}
	return nil
}

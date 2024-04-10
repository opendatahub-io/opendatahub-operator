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

// we stub this rather than mock the client as controller runtime fake client does not support Reactors.
func (mac *stubAudiencesClient) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	if tokenReview, isTokenReview := obj.(*authentication.TokenReview); isTokenReview {
		tokenReview.Status.Audiences = mac.stubAudiences
		tokenReview.Status.Authenticated = true
		return nil
	}
	return nil
}

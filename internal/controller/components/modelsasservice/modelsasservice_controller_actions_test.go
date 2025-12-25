//nolint:testpackage
package modelsasservice

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestGatewayValidation(t *testing.T) {
	g := NewWithT(t)

	t.Run("Gateway Validation", func(t *testing.T) {
		t.Run("should accept valid Gateway with both namespace and name", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					Gateway: componentApi.GatewaySpec{
						Namespace: "valid-namespace",
						Name:      "valid-name",
					},
				},
			}

			rr := &types.ReconciliationRequest{
				Instance: maas,
			}

			err := validateGateway(context.Background(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())
		})

		t.Run("should accept empty Gateway (uses defaults)", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					Gateway: componentApi.GatewaySpec{},
				},
			}

			rr := &types.ReconciliationRequest{
				Instance: maas,
			}

			err := validateGateway(context.Background(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())
		})

		t.Run("should reject Gateway with only namespace specified", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					Gateway: componentApi.GatewaySpec{
						Namespace: "some-namespace",
						Name:      "",
					},
				},
			}

			rr := &types.ReconciliationRequest{
				Instance: maas,
			}

			err := validateGateway(context.Background(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("invalid gateway specification: when specifying a custom gateway, both namespace and name must be provided"))
		})

		t.Run("should reject Gateway with only name specified", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					Gateway: componentApi.GatewaySpec{
						Namespace: "",
						Name:      "some-name",
					},
				},
			}

			rr := &types.ReconciliationRequest{
				Instance: maas,
			}

			err := validateGateway(context.Background(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("invalid gateway specification: when specifying a custom gateway, both namespace and name must be provided"))
		})
	})
}

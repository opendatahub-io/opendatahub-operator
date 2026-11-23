package v2

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
)

// A v2 client cannot represent dataConnectHub in its spec. Exercise a serialized
// read-modify-write through v2 to ensure it preserves the v3-only state while
// still allowing changes to the existing Feast capabilities.
func TestDataConnectHubSurvivesV2Update(t *testing.T) {
	for _, state := range []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed} {
		t.Run("state="+string(state), func(t *testing.T) {
			g := NewWithT(t)
			hub := &dscApi.DataScienceCluster{}
			hub.Annotations = map[string]string{"example": "kept"}
			hub.Spec.Components.Data.FeatureStore.ManagementState = operatorv1.Managed
			hub.Spec.Components.Data.DataRegistry.ManagementState = operatorv1.Removed
			hub.Spec.Components.Data.DataConnectHub.ManagementState = state
			originalHub := hub.DeepCopy()

			legacy := &DataScienceCluster{}
			g.Expect(legacy.ConvertFrom(hub)).To(Succeed())
			g.Expect(hub).To(Equal(originalHub), "conversion must not mutate the source")
			if state == "" {
				g.Expect(legacy.Annotations).NotTo(HaveKey(DataConnectHubV3StateAnnotation))
			} else {
				g.Expect(legacy.Annotations[DataConnectHubV3StateAnnotation]).To(MatchJSON(`{"managementState":"` + string(state) + `"}`))
			}
			g.Expect(legacy.Spec.Components.FeastOperator.ManagementState).To(Equal(operatorv1.Managed))
			g.Expect(legacy.Spec.Components.FeastOperator.DataRegistry.ManagementState).To(Equal(operatorv1.Removed))

			encoded, err := json.Marshal(legacy)
			g.Expect(err).NotTo(HaveOccurred())
			updated := &DataScienceCluster{}
			g.Expect(json.Unmarshal(encoded, updated)).To(Succeed())
			updated.Spec.Components.FeastOperator.ManagementState = operatorv1.Removed
			updated.Spec.Components.FeastOperator.DataRegistry.ManagementState = operatorv1.Managed
			originalUpdate := updated.DeepCopy()

			back := &dscApi.DataScienceCluster{}
			g.Expect(updated.ConvertTo(back)).To(Succeed())
			g.Expect(updated).To(Equal(originalUpdate), "conversion must not mutate the source")
			g.Expect(back.Spec.Components.Data.DataConnectHub.ManagementState).To(Equal(state))
			g.Expect(back.Spec.Components.Data.FeatureStore.ManagementState).To(Equal(operatorv1.Removed))
			g.Expect(back.Spec.Components.Data.DataRegistry.ManagementState).To(Equal(operatorv1.Managed))
			g.Expect(back.Annotations).To(Equal(map[string]string{"example": "kept"}))
		})
	}
}

func TestDataConnectHubConversionRecomputesStaleMarker(t *testing.T) {
	for _, state := range []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed} {
		t.Run("state="+string(state), func(t *testing.T) {
			g := NewWithT(t)
			hub := &dscApi.DataScienceCluster{}
			hub.Annotations = map[string]string{DataConnectHubV3StateAnnotation: "stale"}
			hub.Spec.Components.Data.DataConnectHub.ManagementState = state
			original := hub.DeepCopy()

			legacy := &DataScienceCluster{}
			g.Expect(legacy.ConvertFrom(hub)).To(Succeed())
			if state == "" {
				g.Expect(legacy.Annotations).To(BeNil())
			} else {
				g.Expect(legacy.Annotations[DataConnectHubV3StateAnnotation]).To(MatchJSON(`{"managementState":"` + string(state) + `"}`))
			}
			g.Expect(hub).To(Equal(original))

			back := &dscApi.DataScienceCluster{}
			g.Expect(legacy.ConvertTo(back)).To(Succeed())
			g.Expect(back.Spec.Components.Data.DataConnectHub.ManagementState).To(Equal(state))
			g.Expect(back.Annotations).To(BeNil())
		})
	}
}

func TestDataConnectHubOverridesSurviveV2Update(t *testing.T) {
	g := NewWithT(t)
	hub := &dscApi.DataScienceCluster{}
	hub.Spec.Components.Data.DataConnectHub = componentApi.DSCDataConnectHub{
		ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
		Gateway:        &componentApi.DataConnectHubGateway{Name: "custom-gateway", Namespace: "gateway-ns"},
		TokenReviewAudiences: []string{
			"https://issuer.example/cluster",
			"https://issuer.example/workload",
		},
	}

	legacy := &DataScienceCluster{}
	g.Expect(legacy.ConvertFrom(hub)).To(Succeed())
	encoded, err := json.Marshal(legacy)
	g.Expect(err).NotTo(HaveOccurred())
	updated := &DataScienceCluster{}
	g.Expect(json.Unmarshal(encoded, updated)).To(Succeed())
	updated.Spec.Components.FeastOperator.ManagementState = operatorv1.Managed

	back := &dscApi.DataScienceCluster{}
	g.Expect(updated.ConvertTo(back)).To(Succeed())
	dch := back.Spec.Components.Data.DataConnectHub
	g.Expect(dch.ManagementState).To(Equal(operatorv1.Managed))
	g.Expect(dch.Gateway).To(Equal(hub.Spec.Components.Data.DataConnectHub.Gateway))
	g.Expect(dch.TokenReviewAudiences).To(Equal(hub.Spec.Components.Data.DataConnectHub.TokenReviewAudiences))
}

func TestDataConnectHubConversionAcceptsLegacyScalarMarker(t *testing.T) {
	g := NewWithT(t)
	legacy := &DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			DataConnectHubV3StateAnnotation: string(operatorv1.Managed),
		}},
	}
	back := &dscApi.DataScienceCluster{}
	g.Expect(legacy.ConvertTo(back)).To(Succeed())
	g.Expect(back.Spec.Components.Data.DataConnectHub.ManagementState).To(Equal(operatorv1.Managed))
}

func TestDataConnectHubConversionRejectsInvalidMarker(t *testing.T) {
	for _, value := range []string{"", "Unmanaged", "Force", "managed", "bogus"} {
		t.Run("marker="+value, func(t *testing.T) {
			g := NewWithT(t)
			legacy := &DataScienceCluster{}
			legacy.Annotations = map[string]string{DataConnectHubV3StateAnnotation: value}
			g.Expect(legacy.ConvertTo(&dscApi.DataScienceCluster{})).To(MatchError(ContainSubstring("invalid Data Connect Hub v3 state marker")))
		})
	}
}

func TestDataConnectHubConversionRejectsInvalidState(t *testing.T) {
	g := NewWithT(t)
	hub := &dscApi.DataScienceCluster{}
	hub.Spec.Components.Data.DataConnectHub.ManagementState = operatorv1.Unmanaged
	legacy := &DataScienceCluster{}
	g.Expect(legacy.ConvertFrom(hub)).To(MatchError(ContainSubstring("invalid Data Connect Hub management state")))
}

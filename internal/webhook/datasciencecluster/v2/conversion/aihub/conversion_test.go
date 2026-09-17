package aihub_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

//nolint:dupl // The two directional tests intentionally mirror each other.
func TestAIHubConversionV2ToV3(t *testing.T) {
	g := NewWithT(t)
	source := &dscv2.DataScienceCluster{}
	source.Spec.Components.ModelRegistry.ManagementState = operatorv1.Managed
	source.Spec.Components.ModelRegistry.RegistriesNamespace = "model-registries"
	source.Status.Conditions = []common.Condition{
		{Type: "ModelRegistryReady", Status: metav1.ConditionTrue},
	}
	source.Status.Components.ModelRegistry.ManagementState = operatorv1.Managed
	source.Status.Components.ModelRegistry.ModelRegistryCommonStatus = &componentApi.ModelRegistryCommonStatus{
		RegistriesNamespace: "model-registries",
	}

	hub := &dscv3.DataScienceCluster{}
	g.Expect(source.ConvertTo(hub)).To(Succeed())

	g.Expect(hub.Spec.Components.AIHub.ManagementState).To(Equal(operatorv1.Managed))
	g.Expect(hub.Spec.Components.AIHub.ApplicationNamespace).To(Equal("model-registries"))
	g.Expect(hub.Status.Components.AIHub.ManagementState).To(Equal(operatorv1.Managed))
	g.Expect(hub.Status.Components.AIHub.ApplicationNamespace).To(Equal("model-registries"))
	g.Expect(hub.Status.Conditions).To(ConsistOf(
		MatchFields(IgnoreExtras, Fields{
			"Type":   Equal("AIHubReady"),
			"Status": Equal(metav1.ConditionTrue),
		}),
	))
}

//nolint:dupl // The two directional tests intentionally mirror each other.
func TestAIHubConversionV3ToV2(t *testing.T) {
	g := NewWithT(t)
	source := &dscv3.DataScienceCluster{}
	source.Spec.Components.AIHub.ManagementState = operatorv1.Managed
	source.Spec.Components.AIHub.ApplicationNamespace = "model-registries"
	source.Status.Conditions = []common.Condition{
		{Type: "AIHubReady", Status: metav1.ConditionTrue},
	}
	source.Status.Components.AIHub.ManagementState = operatorv1.Managed
	source.Status.Components.AIHub.AIHubCommonStatus = &componentApi.AIHubCommonStatus{
		ApplicationNamespace: "model-registries",
	}

	destination := &dscv2.DataScienceCluster{}
	g.Expect(destination.ConvertFrom(source)).To(Succeed())

	g.Expect(destination.Spec.Components.ModelRegistry.ManagementState).To(Equal(operatorv1.Managed))
	g.Expect(destination.Spec.Components.ModelRegistry.RegistriesNamespace).To(Equal("model-registries"))
	g.Expect(destination.Status.Components.ModelRegistry.ManagementState).To(Equal(operatorv1.Managed))
	g.Expect(destination.Status.Components.ModelRegistry.RegistriesNamespace).To(Equal("model-registries"))
	g.Expect(destination.Status.Conditions).To(ConsistOf(
		MatchFields(IgnoreExtras, Fields{
			"Type":   Equal("ModelRegistryReady"),
			"Status": Equal(metav1.ConditionTrue),
		}),
	))
}

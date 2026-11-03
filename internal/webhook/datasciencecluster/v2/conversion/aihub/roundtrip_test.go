package aihub_test

import (
	"fmt"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"

	. "github.com/onsi/gomega"
)

func aiHubConditions(conditionType string) []common.Condition {
	heartbeat := metav1.NewTime(time.Unix(1700000060, 0).UTC())
	return []common.Condition{
		{
			Type: conditionType, Status: metav1.ConditionUnknown,
			ObservedGeneration: 7, Reason: "Reconciling", Message: "Registries are starting",
			Severity:           common.ConditionSeverityInfo,
			LastTransitionTime: metav1.NewTime(time.Unix(1700000000, 0).UTC()),
			LastHeartbeatTime:  &heartbeat,
		},
		{Type: "Ready", Status: metav1.ConditionFalse, Reason: "ComponentNotReady"},
	}
}

func aiHubReleases() []common.ComponentRelease {
	return []common.ComponentRelease{
		{Name: "model-registry", Version: "1.2.3", RepoURL: "https://github.com/opendatahub-io/model-registry"},
		{Name: "platform", Version: "3.0.0"},
	}
}

//nolint:dupl // The two directional roundtrips intentionally assert the same preservation contract.
func TestAIHubConversionRoundTrips(t *testing.T) {
	for _, state := range []operatorv1.ManagementState{operatorv1.Managed, operatorv1.Removed, ""} {
		for _, namespace := range []string{registriesNamespace, componentApi.DefaultModelRegistriesNamespace, ""} {
			for _, status := range []struct {
				name     string
				present  bool
				releases []common.ComponentRelease
			}{
				{name: "nil"},
				{name: "nil releases", present: true},
				{name: "empty releases", present: true, releases: []common.ComponentRelease{}},
				{name: "releases", present: true, releases: aiHubReleases()},
			} {
				t.Run(fmt.Sprintf("state=%s/namespace=%s/status=%s", state, namespace, status.name), func(t *testing.T) {
					v2 := &dscv2.DataScienceCluster{}
					v2.Spec.Components.ModelRegistry = componentApi.DSCModelRegistry{
						ManagementSpec:          common.ManagementSpec{ManagementState: state},
						ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{RegistriesNamespace: namespace},
					}
					v2.Status.Components.ModelRegistry.ManagementState = state
					v2.Status.Conditions = aiHubConditions("ModelRegistryReady")
					v3 := &dscApi.DataScienceCluster{}
					v3.Spec.Components.AIHub = componentApi.DSCAIHub{
						ManagementSpec:  common.ManagementSpec{ManagementState: state},
						AIHubCommonSpec: componentApi.AIHubCommonSpec{InstancesNamespace: namespace},
					}
					v3.Status.Components.AIHub.ManagementState = state
					v3.Status.Conditions = aiHubConditions("AIHubReady")
					if status.present {
						v2.Status.Components.ModelRegistry.ModelRegistryCommonStatus = &componentApi.ModelRegistryCommonStatus{
							RegistriesNamespace:    namespace,
							ComponentReleaseStatus: common.ComponentReleaseStatus{Releases: status.releases},
						}
						v3.Status.Components.AIHub.AIHubCommonStatus = &componentApi.AIHubCommonStatus{
							InstancesNamespace:     namespace,
							ComponentReleaseStatus: common.ComponentReleaseStatus{Releases: status.releases},
						}
					}

					t.Run("v2 to v3 to v2", func(t *testing.T) {
						g := NewWithT(t)
						original := v2.DeepCopy()
						hub := &dscApi.DataScienceCluster{}
						g.Expect(v2.ConvertTo(hub)).To(Succeed())
						g.Expect(hub.Spec.Components.AIHub).To(Equal(v3.Spec.Components.AIHub))
						g.Expect(hub.Status.Components.AIHub).To(Equal(v3.Status.Components.AIHub))
						g.Expect(hub.Status.Conditions).To(Equal(v3.Status.Conditions))
						roundtrip := &dscv2.DataScienceCluster{}
						g.Expect(roundtrip.ConvertFrom(hub)).To(Succeed())
						g.Expect(roundtrip.Spec.Components.ModelRegistry).To(Equal(original.Spec.Components.ModelRegistry))
						g.Expect(roundtrip.Status.Components.ModelRegistry).To(Equal(original.Status.Components.ModelRegistry))
						g.Expect(roundtrip.Status.Conditions).To(Equal(original.Status.Conditions))
						hub.Status.Conditions[0].Reason = "Changed"
						hub.Status.Conditions[0].LastHeartbeatTime.Time = time.Unix(0, 0)
						if status.present && len(hub.Status.Components.AIHub.Releases) > 0 {
							hub.Status.Components.AIHub.Releases[0].Version = "changed"
						}
						g.Expect(v2).To(Equal(original), "conversion must not mutate or alias the source")
					})

					t.Run("v3 to v2 to v3", func(t *testing.T) {
						g := NewWithT(t)
						original := v3.DeepCopy()
						spoke := &dscv2.DataScienceCluster{}
						g.Expect(spoke.ConvertFrom(v3)).To(Succeed())
						g.Expect(spoke.Spec.Components.ModelRegistry).To(Equal(v2.Spec.Components.ModelRegistry))
						g.Expect(spoke.Status.Components.ModelRegistry).To(Equal(v2.Status.Components.ModelRegistry))
						g.Expect(spoke.Status.Conditions).To(Equal(v2.Status.Conditions))
						roundtrip := &dscApi.DataScienceCluster{}
						g.Expect(spoke.ConvertTo(roundtrip)).To(Succeed())
						g.Expect(roundtrip.Spec.Components.AIHub).To(Equal(original.Spec.Components.AIHub))
						g.Expect(roundtrip.Status.Components.AIHub).To(Equal(original.Status.Components.AIHub))
						g.Expect(roundtrip.Status.Conditions).To(Equal(original.Status.Conditions))
						spoke.Status.Conditions[0].Reason = "Changed"
						spoke.Status.Conditions[0].LastHeartbeatTime.Time = time.Unix(0, 0)
						if status.present && len(spoke.Status.Components.ModelRegistry.Releases) > 0 {
							spoke.Status.Components.ModelRegistry.Releases[0].Version = "changed"
						}
						g.Expect(v3).To(Equal(original), "conversion must not mutate or alias the source")
					})
				})
			}
		}
	}
}

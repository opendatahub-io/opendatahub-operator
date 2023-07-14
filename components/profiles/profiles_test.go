package profiles_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	dscapi "github.com/opendatahub-io/opendatahub-operator/apis/datasciencecluster/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/components"
	"github.com/opendatahub-io/opendatahub-operator/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/components/workbenches"

	"github.com/opendatahub-io/opendatahub-operator/components/profiles"
)

var _ = Describe("Profiles", func() {
	var baseServingProfile, baseTrainingProfile, baseWorkbenchesProfile, baseFullProfile, baseEmptyProfile *dscapi.DataScienceCluster
	var servingPlusProfile, trainingPlusProfile, workbenchesPlusProfile, fullPlusProfile *dscapi.DataScienceCluster
	var plan *profiles.ReconciliationPlan

	Context("Default profiles without overrides", func() {
		BeforeEach(func() {
			baseServingProfile = &dscapi.DataScienceCluster{
				Spec: dscapi.DataScienceClusterSpec{
					Profile: profiles.ProfileServing,
				},
			}
			baseTrainingProfile = &dscapi.DataScienceCluster{
				Spec: dscapi.DataScienceClusterSpec{
					Profile: profiles.ProfileTraining,
				},
			}
			baseWorkbenchesProfile = &dscapi.DataScienceCluster{
				Spec: dscapi.DataScienceClusterSpec{
					Profile: profiles.ProfileWorkbench,
				},
			}
			baseFullProfile = &dscapi.DataScienceCluster{
				Spec: dscapi.DataScienceClusterSpec{
					Profile: profiles.ProfileCore,
				},
			}
			baseEmptyProfile = &dscapi.DataScienceCluster{}
		})

		It("Serving profile should enable only the serving components", func() {
			plan = profiles.CreateReconciliationPlan(baseServingProfile)

			Expect(plan.Components[modelmeshserving.ComponentName]).To(BeTrue())
			Expect(plan.Components[datasciencepipelines.ComponentName]).To(BeFalse())
			Expect(plan.Components[workbenches.ComponentName]).To(BeFalse())
			Expect(plan.Components[dashboard.ComponentName]).To(BeTrue())
		})
		It("Training profile should enable only the training components", func() {
			plan := profiles.CreateReconciliationPlan(baseTrainingProfile)

			Expect(plan.Components[modelmeshserving.ComponentName]).To(BeFalse())
			Expect(plan.Components[datasciencepipelines.ComponentName]).To(BeTrue())
			Expect(plan.Components[workbenches.ComponentName]).To(BeFalse())
			Expect(plan.Components[dashboard.ComponentName]).To(BeTrue())
		})
		It("Workbenches profile should enable only the workbench components", func() {
			plan := profiles.CreateReconciliationPlan(baseWorkbenchesProfile)

			Expect(plan.Components[modelmeshserving.ComponentName]).To(BeFalse())
			Expect(plan.Components[datasciencepipelines.ComponentName]).To(BeFalse())
			Expect(plan.Components[workbenches.ComponentName]).To(BeTrue())
			Expect(plan.Components[dashboard.ComponentName]).To(BeTrue())
		})
		It("Full profile should enable all components", func() {
			plan := profiles.CreateReconciliationPlan(baseFullProfile)

			Expect(plan.Components[modelmeshserving.ComponentName]).To(BeTrue())
			Expect(plan.Components[datasciencepipelines.ComponentName]).To(BeTrue())
			Expect(plan.Components[workbenches.ComponentName]).To(BeTrue())
			Expect(plan.Components[dashboard.ComponentName]).To(BeTrue())
		})
		It("Empty profile defaults to Full and should enable all components", func() {
			plan := profiles.CreateReconciliationPlan(baseEmptyProfile)

			Expect(plan.Components[modelmeshserving.ComponentName]).To(BeTrue())
			Expect(plan.Components[datasciencepipelines.ComponentName]).To(BeTrue())
			Expect(plan.Components[workbenches.ComponentName]).To(BeTrue())
			Expect(plan.Components[dashboard.ComponentName]).To(BeTrue())
		})
	})

	Context("Profiles with overrides", func() {
		BeforeEach(func() {
			t := true
			f := false
			servingPlusProfile = &dscapi.DataScienceCluster{
				Spec: dscapi.DataScienceClusterSpec{
					Profile: profiles.ProfileServing,
					Components: dscapi.Components{
						ModelMeshServing: modelmeshserving.ModelMeshServing{
							Component: components.Component{Enabled: &f},
						},
						DataSciencePipelines: datasciencepipelines.DataSciencePipelines{
							Component: components.Component{Enabled: &t},
						},
						Workbenches: workbenches.Workbenches{
							Component: components.Component{Enabled: &t},
						},
						Dashboard: dashboard.Dashboard{
							Component: components.Component{Enabled: &f},
						},
					},
				},
			}
			trainingPlusProfile = &dscapi.DataScienceCluster{
				Spec: dscapi.DataScienceClusterSpec{
					Profile: profiles.ProfileTraining,
					Components: dscapi.Components{
						ModelMeshServing: modelmeshserving.ModelMeshServing{
							Component: components.Component{Enabled: &t},
						},
						DataSciencePipelines: datasciencepipelines.DataSciencePipelines{
							Component: components.Component{Enabled: &f},
						},
						Workbenches: workbenches.Workbenches{
							Component: components.Component{Enabled: &t},
						},
						Dashboard: dashboard.Dashboard{
							Component: components.Component{Enabled: &f},
						},
					},
				},
			}
			workbenchesPlusProfile = &dscapi.DataScienceCluster{
				Spec: dscapi.DataScienceClusterSpec{
					Profile: profiles.ProfileWorkbench,
					Components: dscapi.Components{
						ModelMeshServing: modelmeshserving.ModelMeshServing{
							Component: components.Component{Enabled: &t},
						},
						DataSciencePipelines: datasciencepipelines.DataSciencePipelines{
							Component: components.Component{Enabled: &t},
						},
						Workbenches: workbenches.Workbenches{
							Component: components.Component{Enabled: &f},
						},
						Dashboard: dashboard.Dashboard{
							Component: components.Component{Enabled: &f},
						},
					},
				},
			}
			fullPlusProfile = &dscapi.DataScienceCluster{
				Spec: dscapi.DataScienceClusterSpec{
					Profile: profiles.ProfileCore,
					Components: dscapi.Components{
						ModelMeshServing: modelmeshserving.ModelMeshServing{
							Component: components.Component{Enabled: &f},
						},
						DataSciencePipelines: datasciencepipelines.DataSciencePipelines{
							Component: components.Component{Enabled: &f},
						},
						Workbenches: workbenches.Workbenches{
							Component: components.Component{Enabled: &f},
						},
						Dashboard: dashboard.Dashboard{
							Component: components.Component{Enabled: &f},
						},
					},
				},
			}
		})

		It("Serving profile with opposite overrides", func() {
			plan := profiles.CreateReconciliationPlan(servingPlusProfile)

			Expect(plan.Components[modelmeshserving.ComponentName]).To(BeFalse())
			Expect(plan.Components[datasciencepipelines.ComponentName]).To(BeTrue())
			Expect(plan.Components[workbenches.ComponentName]).To(BeTrue())
			Expect(plan.Components[dashboard.ComponentName]).To(BeFalse())
		})
		It("Training profile with opposite overrides", func() {
			plan := profiles.CreateReconciliationPlan(trainingPlusProfile)

			Expect(plan.Components[modelmeshserving.ComponentName]).To(BeTrue())
			Expect(plan.Components[datasciencepipelines.ComponentName]).To(BeFalse())
			Expect(plan.Components[workbenches.ComponentName]).To(BeTrue())
			Expect(plan.Components[dashboard.ComponentName]).To(BeFalse())
		})
		It("Workbench profile with opposite overrides", func() {
			plan := profiles.CreateReconciliationPlan(workbenchesPlusProfile)

			Expect(plan.Components[modelmeshserving.ComponentName]).To(BeTrue())
			Expect(plan.Components[datasciencepipelines.ComponentName]).To(BeTrue())
			Expect(plan.Components[workbenches.ComponentName]).To(BeFalse())
			Expect(plan.Components[dashboard.ComponentName]).To(BeFalse())
		})
		It("Full profile with opposite overrides", func() {
			plan := profiles.CreateReconciliationPlan(fullPlusProfile)

			Expect(plan.Components[modelmeshserving.ComponentName]).To(BeFalse())
			Expect(plan.Components[datasciencepipelines.ComponentName]).To(BeFalse())
			Expect(plan.Components[workbenches.ComponentName]).To(BeFalse())
			Expect(plan.Components[dashboard.ComponentName]).To(BeFalse())
		})
	})

})

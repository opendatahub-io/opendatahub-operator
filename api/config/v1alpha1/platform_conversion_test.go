package v1alpha1

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	v1alpha2 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
)

func TestPlatformConversionPreservesFieldsAndRenamesModules(t *testing.T) {
	g := NewWithT(t)

	legacy := &Platform{
		ObjectMeta: metav1.ObjectMeta{
			Name:        PlatformInstanceName,
			Labels:      map[string]string{"test": "platform"},
			Annotations: map[string]string{"conversion": "preserve"},
			Finalizers:  []string{"test/finalizer"},
		},
		Spec: PlatformSpec{Modules: PlatformModules{
			AIPipelines:          common.ManagementSpec{ManagementState: operatorv1.Managed},
			AIGateway:            common.ManagementSpec{ManagementState: operatorv1.Managed},
			MLflowOperator:       common.ManagementSpec{ManagementState: operatorv1.Removed},
			Monitoring:           common.ManagementSpec{},
			MCPLifecycleOperator: common.ManagementSpec{ManagementState: operatorv1.Managed},
			Kserve:               common.ManagementSpec{ManagementState: operatorv1.Removed},
			Trainer:              common.ManagementSpec{ManagementState: operatorv1.Managed},
			Workbenches:          common.ManagementSpec{},
			OGX:                  common.ManagementSpec{ManagementState: operatorv1.Removed},
			FeastOperator:        common.ManagementSpec{ManagementState: operatorv1.Managed},
			Dashboard:            common.ManagementSpec{ManagementState: operatorv1.Removed},
			SparkOperator:        common.ManagementSpec{ManagementState: operatorv1.Managed},
			Ray:                  common.ManagementSpec{ManagementState: operatorv1.Removed},
			TrustyAI:             common.ManagementSpec{ManagementState: operatorv1.Managed},
			ModelRegistry:        common.ManagementSpec{},
		}},
		Status: PlatformStatus{Status: common.Status{
			Phase:              "Ready",
			ObservedGeneration: 9,
			Conditions: []common.Condition{{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Unix(123, 0)),
				Reason:             "Ready",
				Message:            "all modules are ready",
			}},
		}},
	}

	hub := &v1alpha2.Platform{}
	g.Expect(legacy.ConvertTo(hub)).To(Succeed())
	g.Expect(hub.ObjectMeta).To(Equal(legacy.ObjectMeta))
	g.Expect(hub.Spec.Modules.AIHub.ManagementState).To(Equal(operatorv1.ManagementState("")))
	g.Expect(hub.Spec.Modules.Data.ManagementState).To(Equal(operatorv1.Managed))
	g.Expect(hub.Spec.Modules.AIGateway).To(Equal(legacy.Spec.Modules.AIGateway))
	g.Expect(hub.Spec.Modules.AIPipelines).To(Equal(legacy.Spec.Modules.AIPipelines))
	g.Expect(hub.Spec.Modules.Ray).To(Equal(legacy.Spec.Modules.Ray))
	g.Expect(hub.Spec.Modules.TrustyAI).To(Equal(legacy.Spec.Modules.TrustyAI))
	g.Expect(hub.Spec.Modules.Dashboard).To(Equal(legacy.Spec.Modules.Dashboard))
	g.Expect(hub.Status.Status).To(Equal(legacy.Status.Status))

	encoded, err := json.Marshal(hub.Spec.Modules)
	g.Expect(err).NotTo(HaveOccurred())
	var wire map[string]json.RawMessage
	g.Expect(json.Unmarshal(encoded, &wire)).To(Succeed())
	g.Expect(wire).To(HaveKey("aiHub"))
	g.Expect(wire).To(HaveKey("data"))
	g.Expect(wire).NotTo(HaveKey("modelregistry"))
	g.Expect(wire).NotTo(HaveKey("feastoperator"))

	convertedBack := &Platform{}
	g.Expect(convertedBack.ConvertFrom(hub)).To(Succeed())
	legacy.TypeMeta = metav1.TypeMeta{}
	g.Expect(reflect.DeepEqual(convertedBack, legacy)).To(BeTrue())
}

func TestPlatformConversionPreservesHubManagementStates(t *testing.T) {
	g := NewWithT(t)

	hub := &v1alpha2.Platform{Spec: v1alpha2.PlatformSpec{Modules: v1alpha2.PlatformModules{
		AIPipelines:          common.ManagementSpec{ManagementState: operatorv1.Managed},
		AIHub:                common.ManagementSpec{ManagementState: operatorv1.Removed},
		Data:                 common.ManagementSpec{},
		AIGateway:            common.ManagementSpec{ManagementState: operatorv1.Managed},
		MLflowOperator:       common.ManagementSpec{ManagementState: operatorv1.Removed},
		Monitoring:           common.ManagementSpec{},
		MCPLifecycleOperator: common.ManagementSpec{ManagementState: operatorv1.Managed},
		Kserve:               common.ManagementSpec{ManagementState: operatorv1.Removed},
		Trainer:              common.ManagementSpec{ManagementState: operatorv1.Managed},
		Workbenches:          common.ManagementSpec{},
		OGX:                  common.ManagementSpec{ManagementState: operatorv1.Removed},
		Dashboard:            common.ManagementSpec{ManagementState: operatorv1.Managed},
		SparkOperator:        common.ManagementSpec{ManagementState: operatorv1.Removed},
		Ray:                  common.ManagementSpec{ManagementState: operatorv1.Managed},
		TrustyAI:             common.ManagementSpec{ManagementState: operatorv1.Removed},
	}}}

	legacy := &Platform{}
	g.Expect(legacy.ConvertFrom(hub)).To(Succeed())
	g.Expect(legacy.Spec.Modules.ModelRegistry.ManagementState).To(Equal(operatorv1.Removed))
	g.Expect(legacy.Spec.Modules.FeastOperator.ManagementState).To(Equal(operatorv1.ManagementState("")))
	g.Expect(legacy.Spec.Modules.AIGateway).To(Equal(hub.Spec.Modules.AIGateway))
	g.Expect(legacy.Spec.Modules.AIPipelines).To(Equal(hub.Spec.Modules.AIPipelines))
	g.Expect(legacy.Spec.Modules.Ray).To(Equal(hub.Spec.Modules.Ray))
	g.Expect(legacy.Spec.Modules.TrustyAI).To(Equal(hub.Spec.Modules.TrustyAI))

	convertedHub := &v1alpha2.Platform{}
	g.Expect(legacy.ConvertTo(convertedHub)).To(Succeed())
	hub.TypeMeta = metav1.TypeMeta{}
	g.Expect(convertedHub.Spec).To(Equal(hub.Spec))
}

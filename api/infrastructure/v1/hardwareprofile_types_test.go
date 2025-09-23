package v1

import (
	"testing"

	. "github.com/onsi/gomega"
	infrav1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// TestHardwareProfileSpecStructure validates basic struct construction and field access
// for the v1 HardwareProfile types. This test focuses on Go-level behavior only
// (not CEL validation) and ensures fields are assignable and retrievable as expected.
func TestHardwareProfileSpecStructure(t *testing.T) {
	g := NewWithT(t)

	minCount := intstr.FromInt32(1)
	maxCount := intstr.FromInt32(4)
	def := intstr.FromInt32(2)

	ids := []HardwareIdentifier{
		{
			DisplayName:  "GPU",
			Identifier:   "nvidia.com/gpu",
			MinCount:     minCount,
			MaxCount:     &maxCount,
			DefaultCount: def,
			ResourceType: "Accelerator",
		},
		{
			DisplayName:  "CPU",
			Identifier:   "cpu",
			MinCount:     intstr.FromString("1"),
			DefaultCount: intstr.FromString("2"),
			ResourceType: "CPU",
		},
	}

	// Queue-based scheduling configuration
	queueSpec := &SchedulingSpec{
		SchedulingType: QueueScheduling,
		Kueue: &KueueSchedulingSpec{
			LocalQueueName: "workload-queue",
			PriorityClass:  "high-priority",
		},
	}

	spec := HardwareProfileSpec{
		Identifiers:    ids,
		SchedulingSpec: queueSpec,
	}

	g.Expect(spec.Identifiers).To(HaveLen(2))
	g.Expect(spec.Identifiers[0].DisplayName).To(Equal("GPU"))
	g.Expect(spec.Identifiers[0].Identifier).To(Equal("nvidia.com/gpu"))
	g.Expect(spec.Identifiers[0].MinCount.IntValue()).To(Equal(1))
	g.Expect(spec.Identifiers[0].MaxCount.IntValue()).To(Equal(4))
	g.Expect(spec.Identifiers[0].DefaultCount.IntValue()).To(Equal(2))
	g.Expect(spec.Identifiers[0].ResourceType).To(Equal("Accelerator"))

	g.Expect(spec.Identifiers[1].DisplayName).To(Equal("CPU"))
	g.Expect(spec.Identifiers[1].Identifier).To(Equal("cpu"))
	g.Expect(spec.Identifiers[1].MinCount.String()).To(Equal("1"))
	g.Expect(spec.Identifiers[1].DefaultCount.String()).To(Equal("2"))
	g.Expect(spec.Identifiers[1].ResourceType).To(Equal("CPU"))

	g.Expect(spec.SchedulingSpec).ToNot(BeNil())
	g.Expect(spec.SchedulingSpec.SchedulingType).To(Equal(QueueScheduling))
	g.Expect(spec.SchedulingSpec.Kueue).ToNot(BeNil())
	g.Expect(spec.SchedulingSpec.Kueue.LocalQueueName).To(Equal("workload-queue"))
	g.Expect(spec.SchedulingSpec.Kueue.PriorityClass).To(Equal("high-priority"))
}

// TestHardwareProfileNodeScheduling ensures NodeSchedulingSpec can be set and accessed
// and that typical fields like node selector and tolerations behave as expected.
func TestHardwareProfileNodeScheduling(t *testing.T) {
	g := NewWithT(t)

	nodeSpec := &SchedulingSpec{
		SchedulingType: NodeScheduling,
		Node: &NodeSchedulingSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/arch":             "amd64",
				"node-role.kubernetes.io/worker": "",
			},
			Tolerations: []corev1.Toleration{
				{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
			},
		},
	}

	g.Expect(nodeSpec.Node).ToNot(BeNil())
	g.Expect(nodeSpec.SchedulingType).To(Equal(NodeScheduling))
	g.Expect(nodeSpec.Node.NodeSelector).To(HaveKeyWithValue("kubernetes.io/arch", "amd64"))
	g.Expect(nodeSpec.Node.NodeSelector).To(HaveKey("node-role.kubernetes.io/worker"))
	g.Expect(nodeSpec.Node.Tolerations).To(HaveLen(1))
	g.Expect(nodeSpec.Node.Tolerations[0].Key).To(Equal("nvidia.com/gpu"))
	g.Expect(nodeSpec.Node.Tolerations[0].Operator).To(Equal(corev1.TolerationOpExists))
}

// TestHardwareProfileList validates list behavior for the API list type.
func TestHardwareProfileList(t *testing.T) {
	g := NewWithT(t)

	hpList := HardwareProfileList{
		Items: []HardwareProfile{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "hp1"},
				Spec: HardwareProfileSpec{
					Identifiers: []HardwareIdentifier{{DisplayName: "CPU", Identifier: "cpu", MinCount: intstr.FromInt32(1), DefaultCount: intstr.FromInt32(1), ResourceType: "CPU"}},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "hp2"},
				Spec: HardwareProfileSpec{
					Identifiers: []HardwareIdentifier{{DisplayName: "GPU", Identifier: "nvidia.com/gpu", MinCount: intstr.FromInt32(1), DefaultCount: intstr.FromInt32(1), ResourceType: "Accelerator"}},
				},
			},
		},
	}

	g.Expect(hpList.Items).To(HaveLen(2))
	g.Expect(hpList.Items[0].Name).To(Equal("hp1"))
	g.Expect(hpList.Items[1].Name).To(Equal("hp2"))
}

// TestHardwareProfileDeepCopy verifies that DeepCopy creates an independent copy
// of the HardwareProfile object so that modifications to the copy do not affect
// the original instance.
func TestHardwareProfileDeepCopy(t *testing.T) {
	g := NewWithT(t)

	orig := &HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hp-original",
			Namespace: "default",
		},
		Spec: HardwareProfileSpec{
			Identifiers:    []HardwareIdentifier{{DisplayName: "CPU", Identifier: "cpu", MinCount: intstr.FromInt32(1), DefaultCount: intstr.FromInt32(1), ResourceType: "CPU"}},
			SchedulingSpec: &SchedulingSpec{SchedulingType: QueueScheduling, Kueue: &KueueSchedulingSpec{LocalQueueName: "q1"}},
		},
	}

	dupe := orig.DeepCopy()
	g.Expect(dupe).ToNot(BeIdenticalTo(orig))
	g.Expect(dupe.Name).To(Equal("hp-original"))

	// mutate copy
	dupe.Name = "hp-dupe"
	dupe.Spec.Identifiers[0].DisplayName = "CPU-Changed"
	dupe.Spec.SchedulingSpec.Kueue.PriorityClass = "pc"

	// original remains unchanged
	g.Expect(orig.Name).To(Equal("hp-original"))
	g.Expect(orig.Spec.Identifiers[0].DisplayName).To(Equal("CPU"))
	g.Expect(orig.Spec.SchedulingSpec.Kueue.PriorityClass).To(Equal(""))
}

// TestV1alpha1HardwareProfile
func TestV1alpha1HardwareProfile(t *testing.T) {
	g := NewWithT(t)

	orig := &infrav1alpha1.HardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hp-original",
			Namespace: "default",
		},
		Spec: infrav1alpha1.HardwareProfileSpec{
			Identifiers:    []infrav1alpha1.HardwareIdentifier{{DisplayName: "CPU", Identifier: "cpu", MinCount: intstr.FromInt32(1), DefaultCount: intstr.FromInt32(1), ResourceType: "CPU"}},
			SchedulingSpec: &infrav1alpha1.SchedulingSpec{SchedulingType: infrav1alpha1.QueueScheduling, Kueue: &infrav1alpha1.KueueSchedulingSpec{LocalQueueName: "q1"}},
		},
	}

	dupe := orig.DeepCopy()
	g.Expect(dupe).ToNot(BeIdenticalTo(orig))
	g.Expect(dupe.Name).To(Equal("hp-original"))

	// mutate copy
	dupe.Name = "hp-dupe"
	dupe.Spec.Identifiers[0].DisplayName = "CPU-Changed"
	dupe.Spec.SchedulingSpec.Kueue.PriorityClass = "pc"

	// original remains unchanged
	g.Expect(orig.Name).To(Equal("hp-original"))
	g.Expect(orig.Spec.Identifiers[0].DisplayName).To(Equal("CPU"))
	g.Expect(orig.Spec.SchedulingSpec.Kueue.PriorityClass).To(Equal(""))
}

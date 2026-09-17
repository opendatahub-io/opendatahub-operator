package v1alpha1

import (
	v1alpha2 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts the v1alpha1 compatibility API to the Platform hub.
func (p *Platform) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1alpha2.Platform)
	src := p.DeepCopy()

	dst.ObjectMeta = *src.ObjectMeta.DeepCopy()
	dst.Spec = v1alpha2.PlatformSpec{
		Modules: v1alpha2.PlatformModules{
			AIPipelines:          src.Spec.Modules.AIPipelines,
			AIGateway:            src.Spec.Modules.AIGateway,
			MLflowOperator:       src.Spec.Modules.MLflowOperator,
			Monitoring:           src.Spec.Modules.Monitoring,
			MCPLifecycleOperator: src.Spec.Modules.MCPLifecycleOperator,
			Kserve:               src.Spec.Modules.Kserve,
			Trainer:              src.Spec.Modules.Trainer,
			Workbenches:          src.Spec.Modules.Workbenches,
			OGX:                  src.Spec.Modules.OGX,
			Data:                 src.Spec.Modules.FeastOperator,
			Dashboard:            src.Spec.Modules.Dashboard,
			SparkOperator:        src.Spec.Modules.SparkOperator,
			Ray:                  src.Spec.Modules.Ray,
			TrustyAI:             src.Spec.Modules.TrustyAI,
			AIHub:                src.Spec.Modules.ModelRegistry,
		},
	}
	dst.Status = v1alpha2.PlatformStatus{Status: src.Status.Status}

	return nil
}

// ConvertFrom converts the Platform hub to the v1alpha1 compatibility API.
func (p *Platform) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1alpha2.Platform).DeepCopy()

	p.ObjectMeta = *src.ObjectMeta.DeepCopy()
	p.Spec = PlatformSpec{
		Modules: PlatformModules{
			AIPipelines:          src.Spec.Modules.AIPipelines,
			AIGateway:            src.Spec.Modules.AIGateway,
			MLflowOperator:       src.Spec.Modules.MLflowOperator,
			Monitoring:           src.Spec.Modules.Monitoring,
			MCPLifecycleOperator: src.Spec.Modules.MCPLifecycleOperator,
			Kserve:               src.Spec.Modules.Kserve,
			Trainer:              src.Spec.Modules.Trainer,
			Workbenches:          src.Spec.Modules.Workbenches,
			OGX:                  src.Spec.Modules.OGX,
			FeastOperator:        src.Spec.Modules.Data,
			Dashboard:            src.Spec.Modules.Dashboard,
			SparkOperator:        src.Spec.Modules.SparkOperator,
			Ray:                  src.Spec.Modules.Ray,
			TrustyAI:             src.Spec.Modules.TrustyAI,
			ModelRegistry:        src.Spec.Modules.AIHub,
		},
	}
	p.Status = PlatformStatus{Status: src.Status.Status}

	return nil
}

var _ conversion.Convertible = (*Platform)(nil)

package kserve

import (
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	componentName            = componentApi.KserveComponentName
	serviceMeshOperator      = "servicemeshoperator"
	serverlessOperator       = "serverless-operator"
	kserveConfigMapName      = "inferenceservice-config"
	kserveManifestSourcePath = "overlays/odh"

	// LegacyComponentName is the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.
	LegacyComponentName = "kserve"

	ReadyConditionType = conditionsv1.ConditionType(componentApi.KserveKind + status.ReadySuffix)
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

// Init for set images.
func (s *componentHandler) Init(platform cluster.Platform) error {
	return nil
}

func (s *componentHandler) GetName() string {
	return componentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState {
	if dsc.Spec.Components.Kserve.ManagementState == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}

// for DSC to get compoment Kserve's CR.
func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject {
	return &componentApi.Kserve{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentApi.KserveKind,
			APIVersion: componentApi.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.KserveInstanceName,
			Annotations: map[string]string{
				annotations.ManagementStateAnnotation: string(s.GetManagementState(dsc)),
			},
		},
		Spec: componentApi.KserveSpec{
			KserveCommonSpec: dsc.Spec.Components.Kserve.KserveCommonSpec,
		},
	}
}

func (s *componentHandler) UpdateDSCStatus(dsc *dscv1.DataScienceCluster, obj client.Object) error {
	c, ok := obj.(*componentApi.Kserve)
	if !ok {
		return errors.New("failed to convert to Kserve")
	}

	dsc.Status.InstalledComponents[LegacyComponentName] = false
	dsc.Status.Components.Kserve.ManagementSpec.ManagementState = s.GetManagementState(dsc)
	dsc.Status.Components.Kserve.KserveCommonStatus = nil

	nc := conditionsv1.Condition{
		Type:    ReadyConditionType,
		Status:  corev1.ConditionFalse,
		Reason:  "Unknown",
		Message: "Not Available",
	}

	switch s.GetManagementState(dsc) {
	case operatorv1.Managed:
		dsc.Status.InstalledComponents[LegacyComponentName] = true
		dsc.Status.Components.Kserve.KserveCommonStatus = c.Status.KserveCommonStatus.DeepCopy()

		if rc := meta.FindStatusCondition(c.Status.Conditions, status.ConditionTypeReady); rc != nil {
			nc.Status = corev1.ConditionStatus(rc.Status)
			nc.Reason = rc.Reason
			nc.Message = rc.Message
		}

	case operatorv1.Removed:
		nc.Status = corev1.ConditionFalse
		nc.Reason = string(operatorv1.Removed)
		nc.Message = "Component ManagementState is set to " + string(operatorv1.Removed)

	default:
		return fmt.Errorf("unknown state %s ", s.GetManagementState(dsc))
	}

	conditionsv1.SetStatusCondition(&dsc.Status.Conditions, nc)

	return nil
}

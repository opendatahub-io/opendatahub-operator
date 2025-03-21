package kserve

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
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

	ReadyConditionType = componentApi.KserveKind + status.ReadySuffix
)

var (
	conditionTypes = []string{
		status.ConditionServingAvailable,
		status.ConditionDeploymentsAvailable,
	}
)

var (
	ErrServiceMeshNotConfigured        = odherrors.NewStopError(status.ServiceMeshNotConfiguredMessage)
	ErrServiceMeshMemberAPINotFound    = odherrors.NewStopError(status.ServiceMeshOperatorNotInstalledMessage)
	ErrServiceMeshOperatorNotInstalled = odherrors.NewStopError(status.ServiceMeshOperatorNotInstalledMessage)
	ErrServerlessOperatorNotInstalled  = odherrors.NewStopError(status.ServerlessOperatorNotInstalledMessage)
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

// Init for set images.
func (s *componentHandler) Init(platform common.Platform) error {
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

func (s *componentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	cs := metav1.ConditionUnknown

	c := componentApi.Kserve{}
	c.Name = componentApi.KserveInstanceName

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&c), &c); err != nil && !k8serr.IsNotFound(err) {
		return cs, nil
	}

	dsc, ok := rr.Instance.(*dscv1.DataScienceCluster)
	if !ok {
		return cs, errors.New("failed to convert to DataScienceCluster")
	}

	dsc.Status.InstalledComponents[LegacyComponentName] = false
	dsc.Status.Components.Kserve.ManagementSpec.ManagementState = s.GetManagementState(dsc)
	dsc.Status.Components.Kserve.KserveCommonStatus = nil

	rr.Conditions.MarkFalse(ReadyConditionType)

	switch s.GetManagementState(dsc) {
	case operatorv1.Managed:
		dsc.Status.InstalledComponents[LegacyComponentName] = true
		dsc.Status.Components.Kserve.KserveCommonStatus = c.Status.KserveCommonStatus.DeepCopy()

		if rc := conditions.FindStatusCondition(c.GetStatus(), status.ConditionTypeReady); rc != nil {
			rr.Conditions.MarkFrom(ReadyConditionType, *rc)
			cs = rc.Status
		} else {
			cs = metav1.ConditionFalse
		}

	case operatorv1.Removed:
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(string(operatorv1.Removed)),
			conditions.WithMessage("Component ManagementState is set to %s", operatorv1.Removed),
			conditions.WithSeverity(common.ConditionSeverityInfo),
		)

	default:
		return cs, fmt.Errorf("unknown state %s ", s.GetManagementState(dsc))
	}

	return cs, nil
}

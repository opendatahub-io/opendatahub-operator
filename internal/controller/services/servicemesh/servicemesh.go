package servicemesh

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
)

const (
	ServiceName = "servicemesh"
)

//nolint:gochecknoinits
func init() {
	sr.Add(&serviceHandler{})
}

type serviceHandler struct {
}

func (h *serviceHandler) Init(_ common.Platform) error {
	return nil
}

func (h *serviceHandler) GetName() string {
	return ServiceName
}

func (h *serviceHandler) GetManagementState(_ common.Platform, _ *dsciv1.DSCInitialization) operatorv1.ManagementState {
	return operatorv1.Managed
}

func (h *serviceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	rec := &ServiceMeshReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor("servicemesh-controller"),
	}

	if err := rec.SetupWithManager(ctx, mgr); err != nil {
		return fmt.Errorf("could not create the %s controller: %w", ServiceName, err)
	}

	return nil
}

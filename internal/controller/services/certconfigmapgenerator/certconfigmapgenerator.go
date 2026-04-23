package certconfigmapgenerator

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
)

const (
	ServiceName = "certconfigmapgenerator"
)

func NewHandler() *serviceHandler { return &serviceHandler{} }

type serviceHandler struct {
}

func (h *serviceHandler) Init(_ common.Platform) error {
	return nil
}

func (h *serviceHandler) GetName() string {
	return ServiceName
}

func (h *serviceHandler) GetManagementState(_ common.Platform, _ *dsciv2.DSCInitialization) operatorv1.ManagementState {
	return operatorv1.Managed
}

func (h *serviceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	if err := NewWithManager(ctx, mgr); err != nil {
		return fmt.Errorf("could not create the %s controller: %w", ServiceName, err)
	}

	return nil
}

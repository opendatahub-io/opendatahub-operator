package gateway

import (
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
)

//nolint:gochecknoinits
func init() {
	sr.Add(&ServiceHandler{})
}

// ServiceHandler implements the ServiceHandler interface for Gateway services.
// It manages the lifecycle of GatewayConfig resources and their associated infrastructure.
type ServiceHandler struct{}

// Init initializes the ServiceHandler for the given platform.
// Currently no platform-specific initialization is required.
func (h *ServiceHandler) Init(platform common.Platform) error {
	return nil
}

// GetName returns the service name for this handler.
func (h *ServiceHandler) GetName() string {
	return ServiceName
}

// GetManagementState returns the management state for Gateway services.
// Gateway services are always managed regardless of platform or DSCI configuration.
func (h *ServiceHandler) GetManagementState(platform common.Platform, _ *dsciv2.DSCInitialization) operatorv1.ManagementState {
	return operatorv1.Managed
}

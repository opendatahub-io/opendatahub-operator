package builtin

import (
	"slices"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	aigatewayModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	dashboardModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"
	feastModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/feastoperator"
	kserveModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/kserve"
	mcplifecycleoperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mcplifecycleoperator"
	mlflowOperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mlflowoperator"
	modelregistryModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/modelregistry"
	monitoringModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/monitoring"
	ogxModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/ogx"
	sparkoperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/sparkoperator"
	trainerModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/trainer"
	workbenchesModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

// ModuleRegistration combines a built-in module handler with its orchestration
// metadata.
type ModuleRegistration struct {
	Handler      modules.ModuleHandler
	Runlevel     dag.Runlevel
	ConfigSource modules.ConfigSource
}

var registrations = []ModuleRegistration{
	// dag.RL(20) — core AI/ML components
	{
		Handler:  dashboardModule.NewHandler(),
		Runlevel: dag.RL(20),
	},
	{
		Handler:  mcplifecycleoperatorModule.NewHandler(),
		Runlevel: dag.RL(20),
	},
	{
		Handler:  modelregistryModule.NewHandler(),
		Runlevel: dag.RL(20),
	},
	{
		Handler:  trainerModule.NewHandler(),
		Runlevel: dag.RL(20),
	},
	{
		Handler:  workbenchesModule.NewHandler(),
		Runlevel: dag.RL(20),
	},
	{
		Handler:      monitoringModule.NewHandler(),
		Runlevel:     dag.RL(20),
		ConfigSource: modules.ConfigFromDSCI,
	},

	// dag.RL(31) — first extension sub-tier
	{
		Handler:  kserveModule.NewHandler(),
		Runlevel: dag.RL(31),
	},

	// dag.RL(32) — second extension sub-tier
	{
		Handler:  aigatewayModule.NewHandler(),
		Runlevel: dag.RL(32),
	},
	{
		Handler:  feastModule.NewHandler(),
		Runlevel: dag.RL(32),
	},
	{
		Handler:  mlflowOperatorModule.NewHandler(),
		Runlevel: dag.RL(32),
	},
	{
		Handler:  ogxModule.NewHandler(),
		Runlevel: dag.RL(32),
	},
	{
		Handler:  sparkoperatorModule.NewHandler(),
		Runlevel: dag.RL(32),
	},
}

// Registrations returns built-in module registrations in provisioning order.
func Registrations() []ModuleRegistration {
	return slices.Clone(registrations)
}

// Names returns built-in module names in provisioning order.
func Names() []string {
	names := make([]string, 0, len(registrations))
	for _, registration := range registrations {
		names = append(names, registration.Handler.GetName())
	}
	return names
}

// Register adds every built-in module handler and its orchestration metadata
// to reg.
func Register(reg *modules.Registry) {
	for _, registration := range registrations {
		reg.Add(
			registration.Handler,
			modules.WithRunlevel(registration.Runlevel),
			modules.WithConfigSource(registration.ConfigSource),
		)
	}
}

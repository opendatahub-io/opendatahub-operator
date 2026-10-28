package builtin

import (
	"slices"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	aigatewayModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	aihubModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aihub"
	aipipelinesModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aipipelines"
	dashboardModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"
	dataModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/data"
	databaseserviceModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/databaseservice"
	kserveModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/kserve"
	mcplifecycleoperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mcplifecycleoperator"
	mlflowOperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mlflowoperator"
	monitoringModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/monitoring"
	ogxModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/ogx"
	rayModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/ray"
	sparkoperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/sparkoperator"
	trainerModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/trainer"
	trustyaiModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/trustyai"
	workbenchesModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

// ModuleRegistration combines a built-in module handler with its orchestration
// metadata.
type ModuleRegistration struct {
	Handler       modules.ModuleHandler
	Runlevel      dag.Runlevel
	ConfigSource  modules.ConfigSource
	// BundleCRDName is the primary module CRD installed by the OLM bundle.
	// Empty means the release does not bundle-own this optional module CRD.
	BundleCRDName string
}

var registrations = []ModuleRegistration{
	// dag.RL(20) — core AI/ML components
	{
		Handler:       aipipelinesModule.NewHandler(),
		Runlevel:      dag.RL(20),
		BundleCRDName: "aipipelines.components.platform.opendatahub.io",
	},
	{
		Handler:       dashboardModule.NewHandler(),
		Runlevel:      dag.RL(20),
		BundleCRDName: "dashboards.components.platform.opendatahub.io",
	},
	{
		// Removed in the 3.6 release defaults; not bundle-owned.
		Handler:  databaseserviceModule.NewHandler(),
		Runlevel: dag.RL(20),
	},
	{
		Handler:       mcplifecycleoperatorModule.NewHandler(),
		Runlevel:      dag.RL(20),
		BundleCRDName: "mcplifecycleoperators.components.platform.opendatahub.io",
	},
	{
		Handler:       aihubModule.NewHandler(),
		Runlevel:      dag.RL(20),
		BundleCRDName: "aihubs.components.platform.opendatahub.io",
	},
	{
		Handler:       rayModule.NewHandler(),
		Runlevel:      dag.RL(20),
		BundleCRDName: "rays.components.platform.opendatahub.io",
	},
	{
		Handler:       trainerModule.NewHandler(),
		Runlevel:      dag.RL(20),
		BundleCRDName: "trainers.components.platform.opendatahub.io",
	},
	{
		Handler:       workbenchesModule.NewHandler(),
		Runlevel:      dag.RL(20),
		BundleCRDName: "workbenches.components.platform.opendatahub.io",
	},
	{
		Handler:       monitoringModule.NewHandler(),
		Runlevel:      dag.RL(20),
		ConfigSource:  modules.ConfigFromDSCI,
		BundleCRDName: "monitorings.services.platform.opendatahub.io",
	},

	// dag.RL(31) — first extension sub-tier
	{
		Handler:       kserveModule.NewHandler(),
		Runlevel:      dag.RL(31),
		BundleCRDName: "kserves.components.platform.opendatahub.io",
	},

	// dag.RL(32) — second extension sub-tier
	{
		Handler:       aigatewayModule.NewHandler(),
		Runlevel:      dag.RL(32),
		BundleCRDName: "aigateways.components.platform.opendatahub.io",
	},
	{
		Handler:       dataModule.NewHandler(),
		Runlevel:      dag.RL(32),
		BundleCRDName: "feastoperators.components.platform.opendatahub.io",
	},
	{
		Handler:       mlflowOperatorModule.NewHandler(),
		Runlevel:      dag.RL(32),
		BundleCRDName: "mlflowoperators.components.platform.opendatahub.io",
	},
	{
		Handler:       ogxModule.NewHandler(),
		Runlevel:      dag.RL(32),
		BundleCRDName: "ogxs.components.platform.opendatahub.io",
	},
	{
		// Removed in the 3.6 release defaults; not bundle-owned.
		Handler:  sparkoperatorModule.NewHandler(),
		Runlevel: dag.RL(32),
	},
	{
		Handler:       trustyaiModule.NewHandler(),
		Runlevel:      dag.RL(32),
		BundleCRDName: "trustyais.components.platform.opendatahub.io",
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

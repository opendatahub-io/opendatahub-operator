package builtin

import (
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	aigatewayModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	dashboardModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"
	feastModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/feastoperator"
	kserveModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/kserve"
	mcplifecycleoperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mcplifecycleoperator"
	mlflowOperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mlflowoperator"
	modelregistryModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/modelregistry"
	ogxModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/ogx"
	sparkoperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/sparkoperator"
	trainerModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/trainer"
	workbenchesModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

type moduleEntry struct {
	handler  modules.ModuleHandler
	runlevel dag.Runlevel
}

// entries is the single registration source for built-in module handlers and
// their DAG runlevels. cmd/main.go and tests derive Handlers() and
// Runlevels() from here.
func entries() map[string]moduleEntry {
	return map[string]moduleEntry{
		// dag.RL(20) — core AI/ML components
		componentApi.DashboardComponentName: {
			handler:  dashboardModule.NewHandler(),
			runlevel: dag.RL(20),
		},
		componentApi.MCPLifecycleOperatorComponentName: {
			handler:  mcplifecycleoperatorModule.NewHandler(),
			runlevel: dag.RL(20),
		},
		componentApi.ModelRegistryComponentName: {
			handler:  modelregistryModule.NewHandler(),
			runlevel: dag.RL(20),
		},
		componentApi.TrainerComponentName: {
			handler:  trainerModule.NewHandler(),
			runlevel: dag.RL(20),
		},
		componentApi.WorkbenchesComponentName: {
			handler:  workbenchesModule.NewHandler(),
			runlevel: dag.RL(20),
		},
		// Monitoring remains deployed via existingServices until modularized.
		// serviceApi.MonitoringServiceName: {
		// 	handler:  monitoringModule.NewHandler(),
		// 	runlevel: dag.RL(20),
		// },

		// dag.RL(31) — first extension sub-tier
		componentApi.KserveComponentName: {
			handler:  kserveModule.NewHandler(),
			runlevel: dag.RL(31),
		},

		// dag.RL(32) — second extension sub-tier
		componentApi.AIGatewayComponentName: {
			handler:  aigatewayModule.NewHandler(),
			runlevel: dag.RL(32),
		},
		componentApi.FeastOperatorComponentName: {
			handler:  feastModule.NewHandler(),
			runlevel: dag.RL(32),
		},
		componentApi.MLflowOperatorComponentName: {
			handler:  mlflowOperatorModule.NewHandler(),
			runlevel: dag.RL(32),
		},
		componentApi.OGXComponentName: {
			handler:  ogxModule.NewHandler(),
			runlevel: dag.RL(32),
		},
		componentApi.SparkOperatorComponentName: {
			handler:  sparkoperatorModule.NewHandler(),
			runlevel: dag.RL(32),
		},
	}
}

// Handlers returns every module handler registered by the platform operator.
func Handlers() map[string]modules.ModuleHandler {
	all := entries()
	handlers := make(map[string]modules.ModuleHandler, len(all))
	for name, entry := range all {
		handlers[name] = entry.handler
	}
	return handlers
}

// Runlevels returns the DAG runlevel for each built-in module handler.
func Runlevels() map[string]dag.Runlevel {
	all := entries()
	runlevels := make(map[string]dag.Runlevel, len(all))
	for name, entry := range all {
		runlevels[name] = entry.runlevel
	}
	return runlevels
}

// Register adds every built-in module handler to reg with its runlevel.
func Register(reg *modules.Registry) {
	for _, entry := range entries() {
		reg.Add(entry.handler, modules.WithRunlevel(entry.runlevel))
	}
}

package modules

import (
	"context"
	"fmt"
	"maps"
	"time"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const moduleGatePreflightErrorsKey = "odh.io/module-gate-preflight-errors"

// ModuleGatePreflightErrors returns gate discovery failures by module name.
func ModuleGatePreflightErrors(rr *odhtype.ReconciliationRequest) map[string]string {
	failures, _ := rr.Extensions[moduleGatePreflightErrorsKey].(map[string]string)
	return failures
}

// checkPlatformModuleUpgradeGates preserves the pre-tracker atomic gate
// barrier for discovered gates. Each managed module is rendered independently,
// so a broken module cannot hide healthy siblings' gates or block their trackers.
//
// The temporary request is never passed to deploy. Its resources exist only
// for gate extraction; the real request remains limited to tracker CRs.
func checkPlatformModuleUpgradeGates(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	delete(rr.Extensions, moduleGatePreflightErrorsKey)
	if !DefaultRegistry().HasEntries() {
		return nil
	}
	platform, ok := rr.Instance.(*configApi.Platform)
	if !ok {
		return fmt.Errorf("module gate preflight requires Platform instance, got %T", rr.Instance)
	}
	gateEntries, failures, err := DiscoverModuleUpgradeGates(ctx, rr, platform, "")
	if err != nil || gateEntries == nil {
		return err
	}
	if len(failures) > 0 {
		if rr.Extensions == nil {
			rr.Extensions = make(map[string]any)
		}
		rr.Extensions[moduleGatePreflightErrorsKey] = failures
	}
	if err := provision.CheckUpgradeGates(ctx, rr.Client, rr.Release, rr.Conditions, gateEntries); err != nil {
		return err
	}
	if len(failures) > 0 {
		return odherrors.NewRequeueAfterError(30 * time.Second)
	}
	return nil
}

// DiscoverModuleUpgradeGates renders the enabled modules to find their chart
// gates. If the caller already rendered one module, use its extracted gates.
func DiscoverModuleUpgradeGates(
	ctx context.Context, rr *odhtype.ReconciliationRequest, platform *configApi.Platform, renderedModule string,
) (map[string]string, map[string]string, error) {
	if !DefaultRegistry().HasEntries() {
		return rr.GateEntries, nil, nil
	}
	gateContext := newGateRenderRequest(rr)
	gateContext.Instance = platform
	platformCtx, err := buildPlatformContext(ctx, &gateContext)
	if err != nil {
		return nil, nil, err
	}

	var gateEntries map[string]string
	if renderedModule != "" {
		gateEntries = make(map[string]string)
		maps.Copy(gateEntries, rr.GateEntries)
	}
	failures := make(map[string]string)
	err = DefaultRegistry().ForAll(func(handler ModuleHandler, registryEnabled bool) error {
		if !registryEnabled || !handler.IsEnabled(platformCtx.Modules) {
			return nil
		}
		if gateEntries == nil {
			gateEntries = make(map[string]string)
		}
		if handler.GetName() == renderedModule {
			return nil
		}
		// ponytail: render siblings at deployment time; cache by render inputs if this becomes expensive.
		gateRR := newGateRenderRequest(&gateContext)
		manifests := handler.GetOperatorManifests(platformCtx)
		gateRR.HelmCharts = manifests.HelmCharts
		gateRR.Manifests = manifests.Manifests
		if len(gateRR.HelmCharts) > 0 {
			if err := helm.NewAction()(ctx, &gateRR); err != nil {
				failures[handler.GetName()] = fmt.Sprintf("rendering module Helm charts for upgrade gates: %v", err)
				return nil
			}
		}
		if len(gateRR.Manifests) > 0 {
			if err := kustomize.NewAction()(ctx, &gateRR); err != nil {
				failures[handler.GetName()] = fmt.Sprintf("rendering module Kustomize manifests for upgrade gates: %v", err)
				return nil
			}
		}
		if err := provision.ExtractUpgradeGates(ctx, &gateRR); err != nil {
			failures[handler.GetName()] = fmt.Sprintf("extracting module upgrade gates: %v", err)
			return nil
		}
		maps.Copy(gateEntries, gateRR.GateEntries)
		return nil
	})
	return gateEntries, failures, err
}

// newGateRenderRequest returns an isolated request containing only context
// required by render actions. Mutable render/deploy state is intentionally
// omitted; future ReconciliationRequest fields must be added here explicitly
// when gate preflight needs them.
func newGateRenderRequest(rr *odhtype.ReconciliationRequest) odhtype.ReconciliationRequest {
	return odhtype.ReconciliationRequest{
		Client:            rr.Client,
		Controller:        rr.Controller,
		Conditions:        rr.Conditions,
		Instance:          rr.Instance,
		Release:           rr.Release,
		ManifestsBasePath: rr.ManifestsBasePath,
		ChartsBasePath:    rr.ChartsBasePath,
	}
}

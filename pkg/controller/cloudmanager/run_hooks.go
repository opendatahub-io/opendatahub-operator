package cloudmanager

import (
	"context"
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// runHooks iterates over rr.HelmCharts and executes the hook functions
// extracted by getHooks for each chart. Nil hooks are skipped. Execution
// stops on the first error, which is wrapped with the chart release name.
func runHooks(
	ctx context.Context,
	rr *types.ReconciliationRequest,
	getHooks func(*types.HelmChartInfo) []types.HookFn,
) error {
	for i := range rr.HelmCharts {
		for _, fn := range getHooks(&rr.HelmCharts[i]) {
			if fn == nil {
				continue
			}
			if err := fn(ctx, rr); err != nil {
				return fmt.Errorf("hook %q failed: %w", rr.HelmCharts[i].ReleaseName, err)
			}
		}
	}
	return nil
}

//nolint:testpackage // testing unexported methods
package cloudmanager

import (
	"context"
	"errors"
	"testing"

	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func preApplyGetter(c *types.HelmChartInfo) []types.HookFn {
	return c.PreApply
}

func postApplyGetter(c *types.HelmChartInfo) []types.HookFn {
	return c.PostApply
}

func TestRunHooks(t *testing.T) {
	t.Run("executes hooks in order", func(t *testing.T) {
		var order []string

		rr := &types.ReconciliationRequest{
			HelmCharts: []types.HelmChartInfo{
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-1",
					},
					PreApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							order = append(order, "chart-1")
							return nil
						},
					},
				},
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-2",
					},
					PreApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							order = append(order, "chart-2")
							return nil
						},
					},
				},
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-3",
					},
					PreApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							order = append(order, "chart-3")
							return nil
						},
					},
				},
			},
		}

		err := runHooks(context.Background(), rr, preApplyGetter)

		require.NoError(t, err)
		assert.Equal(t, []string{"chart-1", "chart-2", "chart-3"}, order)
	})

	t.Run("executes multiple hooks per chart in order", func(t *testing.T) {
		var order []string

		rr := &types.ReconciliationRequest{
			HelmCharts: []types.HelmChartInfo{
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-1",
					},
					PreApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							order = append(order, "chart-1-hook-1")
							return nil
						},
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							order = append(order, "chart-1-hook-2")
							return nil
						},
					},
				},
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-2",
					},
					PreApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							order = append(order, "chart-2-hook-1")
							return nil
						},
					},
				},
			},
		}

		err := runHooks(context.Background(), rr, preApplyGetter)

		require.NoError(t, err)
		assert.Equal(t, []string{"chart-1-hook-1", "chart-1-hook-2", "chart-2-hook-1"}, order)
	})

	t.Run("stops on first error and propagates it", func(t *testing.T) {
		sentinel := errors.New("hook failed")
		var executed []string

		rr := &types.ReconciliationRequest{
			HelmCharts: []types.HelmChartInfo{
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-1",
					},
					PreApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							executed = append(executed, "chart-1")
							return nil
						},
					},
				},
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-2",
					},
					PreApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							executed = append(executed, "chart-2")
							return sentinel
						},
					},
				},
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-3",
					},
					PreApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							executed = append(executed, "chart-3")
							return nil
						},
					},
				},
			},
		}

		err := runHooks(context.Background(), rr, preApplyGetter)

		require.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"chart-1", "chart-2"}, executed)
	})

	t.Run("stops on first error within a chart hooks", func(t *testing.T) {
		sentinel := errors.New("hook failed")
		var executed []string

		rr := &types.ReconciliationRequest{
			HelmCharts: []types.HelmChartInfo{
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-1",
					},
					PreApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							executed = append(executed, "hook-1")
							return nil
						},
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							executed = append(executed, "hook-2")
							return sentinel
						},
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							executed = append(executed, "hook-3")
							return nil
						},
					},
				},
			},
		}

		err := runHooks(context.Background(), rr, preApplyGetter)

		require.ErrorIs(t, err, sentinel)
		assert.Equal(t, []string{"hook-1", "hook-2"}, executed)
	})

	t.Run("skips nil hooks", func(t *testing.T) {
		var executed []string

		rr := &types.ReconciliationRequest{
			HelmCharts: []types.HelmChartInfo{
				{Source: helmRenderer.Source{
					ReleaseName: "chart-1",
				}, PreApply: nil},
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-2",
					},
					PreApply: []types.HookFn{
						nil,
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							executed = append(executed, "chart-2")
							return nil
						},
						nil,
					},
				},
				{Source: helmRenderer.Source{
					ReleaseName: "chart-3",
				}, PreApply: nil},
			},
		}

		err := runHooks(context.Background(), rr, preApplyGetter)

		require.NoError(t, err)
		assert.Equal(t, []string{"chart-2"}, executed)
	})

	t.Run("handles empty HelmCharts slice", func(t *testing.T) {
		rr := &types.ReconciliationRequest{
			HelmCharts: []types.HelmChartInfo{},
		}

		err := runHooks(context.Background(), rr, preApplyGetter)

		require.NoError(t, err)
	})

	t.Run("works for post-apply hooks", func(t *testing.T) {
		var order []string

		rr := &types.ReconciliationRequest{
			HelmCharts: []types.HelmChartInfo{
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-1",
					},
					PostApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							order = append(order, "chart-1")
							return nil
						},
					},
				},
				{
					Source: helmRenderer.Source{
						ReleaseName: "chart-2",
					},
					PostApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							order = append(order, "chart-2")
							return nil
						},
					},
				},
			},
		}

		err := runHooks(context.Background(), rr, postApplyGetter)

		require.NoError(t, err)
		assert.Equal(t, []string{"chart-1", "chart-2"}, order)
	})

	t.Run("wraps error with release name", func(t *testing.T) {
		sentinel := errors.New("something broke")

		rr := &types.ReconciliationRequest{
			HelmCharts: []types.HelmChartInfo{
				{
					Source: helmRenderer.Source{
						ReleaseName: "my-chart",
					},
					PostApply: []types.HookFn{
						func(_ context.Context, _ *types.ReconciliationRequest) error {
							return sentinel
						},
					},
				},
			},
		}

		err := runHooks(context.Background(), rr, postApplyGetter)

		require.ErrorIs(t, err, sentinel)
		assert.Contains(t, err.Error(), `hook "my-chart" failed`)
	})
}

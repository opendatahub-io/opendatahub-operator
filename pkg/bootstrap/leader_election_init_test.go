/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bootstrap_test

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	bootstrap "github.com/opendatahub-io/opendatahub-operator/v2/pkg/bootstrap"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

func newHooks(order *[]string) bootstrap.LeaderElectionInitHooks {
	return bootstrap.LeaderElectionInitHooks{
		CleanupExistingResource: func(_ context.Context, _ client.Client) error {
			*order = append(*order, "cleanup")
			return nil
		},
		CreateDefaultDSCI: func(_ context.Context, _ client.Client, _ common.Platform, _ string) error {
			*order = append(*order, "dsci")
			return nil
		},
		CreateDefaultDSC: func(_ context.Context, _ client.Client) error {
			*order = append(*order, "dsc")
			return nil
		},
	}
}

func runAndRequireOrder(t *testing.T, cfg bootstrap.LeaderElectionInitConfig, expected []string) {
	t.Helper()

	ctx := context.Background()
	log := testr.New(t)
	cli := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()

	var order []string
	err := bootstrap.RunLeaderElectionInit(ctx, log, cli, cfg, newHooks(&order))
	require.NoError(t, err)
	require.Equal(t, expected, order)
}

func TestRunLeaderElectionInit_ReturnsErrorWhenHookNil(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := testr.New(t)
	cli := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:             cluster.ManagedRhoai,
		MonitoringNamespace:  "mon",
		DSCIEnabled:          true,
		DSCEnabled:           true,
		DisableDSCAutoCreate: false,
	}

	ok := func(_ context.Context, _ client.Client) error { return nil }
	okDSCI := func(_ context.Context, _ client.Client, _ common.Platform, _ string) error { return nil }
	okDSC := func(_ context.Context, _ client.Client) error { return nil }

	tests := []struct {
		name  string
		hooks bootstrap.LeaderElectionInitHooks
		want  string
	}{
		{
			name: "CleanupExistingResource",
			hooks: bootstrap.LeaderElectionInitHooks{
				CleanupExistingResource: nil,
				CreateDefaultDSCI:       okDSCI,
				CreateDefaultDSC:        okDSC,
			},
			want: "LeaderElectionInitHooks.CleanupExistingResource is nil",
		},
		{
			name: "CreateDefaultDSCI",
			hooks: bootstrap.LeaderElectionInitHooks{
				CleanupExistingResource: ok,
				CreateDefaultDSCI:       nil,
				CreateDefaultDSC:        okDSC,
			},
			want: "LeaderElectionInitHooks.CreateDefaultDSCI is nil",
		},
		{
			name: "CreateDefaultDSC",
			hooks: bootstrap.LeaderElectionInitHooks{
				CleanupExistingResource: ok,
				CreateDefaultDSCI:       okDSCI,
				CreateDefaultDSC:        nil,
			},
			want: "LeaderElectionInitHooks.CreateDefaultDSC is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := bootstrap.RunLeaderElectionInit(ctx, log, cli, cfg, tt.hooks)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestRunLeaderElectionInit_OrderFullPath(t *testing.T) {
	t.Parallel()

	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:             cluster.ManagedRhoai,
		MonitoringNamespace:  "mon",
		DSCIEnabled:          true,
		DSCEnabled:           true,
		DisableDSCAutoCreate: false,
	}

	runAndRequireOrder(t, cfg, []string{"cleanup", "dsci", "dsc"})
}

func TestRunLeaderElectionInit_SkipsDSCIWhenControllerDisabled(t *testing.T) {
	t.Parallel()

	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:             cluster.ManagedRhoai,
		DSCIEnabled:          false,
		DSCEnabled:           true,
		DisableDSCAutoCreate: false,
	}

	runAndRequireOrder(t, cfg, []string{"cleanup", "dsc"})
}

func TestRunLeaderElectionInit_SkipsDSCIWhenAutoCreateDisabled(t *testing.T) {
	t.Parallel()

	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:             cluster.ManagedRhoai,
		DSCIEnabled:          true,
		DSCEnabled:           true,
		DisableDSCAutoCreate: true,
	}

	runAndRequireOrder(t, cfg, []string{"cleanup", "dsc"})
}

func TestRunLeaderElectionInit_SkipsDSCWhenNotManagedRhoai(t *testing.T) {
	t.Parallel()

	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:             cluster.SelfManagedRhoai,
		DSCIEnabled:          true,
		DSCEnabled:           true,
		DisableDSCAutoCreate: false,
	}

	runAndRequireOrder(t, cfg, []string{"cleanup", "dsci"})
}

func TestRunLeaderElectionInit_SkipsDSCWhenDSCDisabled(t *testing.T) {
	t.Parallel()

	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:             cluster.ManagedRhoai,
		DSCIEnabled:          true,
		DSCEnabled:           false,
		DisableDSCAutoCreate: false,
	}

	runAndRequireOrder(t, cfg, []string{"cleanup", "dsci"})
}

func TestRunLeaderElectionInit_ReturnsErrorAndStopsAfterCleanupFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := testr.New(t)
	cli := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()

	cleanupErr := errors.New("cleanup failed")
	var order []string
	hooks := bootstrap.LeaderElectionInitHooks{
		CleanupExistingResource: func(_ context.Context, _ client.Client) error {
			order = append(order, "cleanup")
			return cleanupErr
		},
		CreateDefaultDSCI: func(_ context.Context, _ client.Client, _ common.Platform, _ string) error {
			order = append(order, "dsci")
			return nil
		},
		CreateDefaultDSC: func(_ context.Context, _ client.Client) error {
			order = append(order, "dsc")
			return nil
		},
	}

	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:             cluster.ManagedRhoai,
		DSCIEnabled:          true,
		DSCEnabled:           true,
		DisableDSCAutoCreate: false,
	}

	err := bootstrap.RunLeaderElectionInit(ctx, log, cli, cfg, hooks)
	require.ErrorIs(t, err, cleanupErr)
	require.Equal(t, []string{"cleanup"}, order)
}

func TestRunLeaderElectionInit_ReturnsErrorAndStopsAfterDSCIFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := testr.New(t)
	cli := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()

	dsciErr := errors.New("dsci failed")
	var order []string
	hooks := bootstrap.LeaderElectionInitHooks{
		CleanupExistingResource: func(_ context.Context, _ client.Client) error {
			order = append(order, "cleanup")
			return nil
		},
		CreateDefaultDSCI: func(_ context.Context, _ client.Client, _ common.Platform, _ string) error {
			order = append(order, "dsci")
			return dsciErr
		},
		CreateDefaultDSC: func(_ context.Context, _ client.Client) error {
			order = append(order, "dsc")
			return nil
		},
	}

	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:             cluster.ManagedRhoai,
		DSCIEnabled:          true,
		DSCEnabled:           true,
		DisableDSCAutoCreate: false,
	}

	err := bootstrap.RunLeaderElectionInit(ctx, log, cli, cfg, hooks)
	require.ErrorIs(t, err, dsciErr)
	require.Equal(t, []string{"cleanup", "dsci"}, order)
}

func TestRunLeaderElectionInit_ReturnsErrorOnDSCFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := testr.New(t)
	cli := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()

	dscErr := errors.New("dsc failed")
	var order []string
	hooks := bootstrap.LeaderElectionInitHooks{
		CleanupExistingResource: func(_ context.Context, _ client.Client) error {
			order = append(order, "cleanup")
			return nil
		},
		CreateDefaultDSCI: func(_ context.Context, _ client.Client, _ common.Platform, _ string) error {
			order = append(order, "dsci")
			return nil
		},
		CreateDefaultDSC: func(_ context.Context, _ client.Client) error {
			order = append(order, "dsc")
			return dscErr
		},
	}

	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:             cluster.ManagedRhoai,
		DSCIEnabled:          true,
		DSCEnabled:           true,
		DisableDSCAutoCreate: false,
	}

	err := bootstrap.RunLeaderElectionInit(ctx, log, cli, cfg, hooks)
	require.ErrorIs(t, err, dscErr)
	require.Equal(t, []string{"cleanup", "dsci", "dsc"}, order)
}

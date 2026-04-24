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

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/bootstrap"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// MockClient is a mock implementation of client.Client for testing.
type MockClient struct {
	mock.Mock
	client.Client
}

// TestDefaultLeaderElectionInitHooks tests that the default hooks are properly initialized.
func TestDefaultLeaderElectionInitHooks(t *testing.T) {
	hooks := bootstrap.DefaultLeaderElectionInitHooks()

	assert.NotNil(t, hooks.CleanupExistingResource, "CleanupExistingResource hook should not be nil")
	assert.NotNil(t, hooks.CreateDefaultDSCI, "CreateDefaultDSCI hook should not be nil")
	assert.NotNil(t, hooks.CreateDefaultDSC, "CreateDefaultDSC hook should not be nil")
}

// TestValidateLeaderElectionInitHooks tests the hook validation function through RunLeaderElectionInit.
func TestValidateLeaderElectionInitHooks(t *testing.T) {
	ctx := context.Background()
	log := logr.Discard()
	mockClient := &MockClient{}

	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:                  cluster.OpenDataHub,
		ManifestsBasePath:         "/test/manifests",
		MonitoringNamespace:       "test-monitoring",
		DSCIEnabled:               true,
		DSCEnabled:                false,
		CreateDefaultDSCIDisabled: false,
	}

	tests := []struct {
		name    string
		hooks   bootstrap.LeaderElectionInitHooks
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid hooks",
			hooks: bootstrap.LeaderElectionInitHooks{
				CleanupExistingResource: func(context.Context, client.Client, string) error { return nil },
				CreateDefaultDSCI:       func(context.Context, client.Client, common.Platform, string) error { return nil },
				CreateDefaultDSC:        func(context.Context, client.Client) error { return nil },
			},
			wantErr: false,
		},
		{
			name: "nil CleanupExistingResource",
			hooks: bootstrap.LeaderElectionInitHooks{
				CleanupExistingResource: nil,
				CreateDefaultDSCI:       func(context.Context, client.Client, common.Platform, string) error { return nil },
				CreateDefaultDSC:        func(context.Context, client.Client) error { return nil },
			},
			wantErr: true,
			errMsg:  "LeaderElectionInitHooks.CleanupExistingResource is nil",
		},
		{
			name: "nil CreateDefaultDSCI",
			hooks: bootstrap.LeaderElectionInitHooks{
				CleanupExistingResource: func(context.Context, client.Client, string) error { return nil },
				CreateDefaultDSCI:       nil,
				CreateDefaultDSC:        func(context.Context, client.Client) error { return nil },
			},
			wantErr: true,
			errMsg:  "LeaderElectionInitHooks.CreateDefaultDSCI is nil",
		},
		{
			name: "nil CreateDefaultDSC",
			hooks: bootstrap.LeaderElectionInitHooks{
				CleanupExistingResource: func(context.Context, client.Client, string) error { return nil },
				CreateDefaultDSCI:       func(context.Context, client.Client, common.Platform, string) error { return nil },
				CreateDefaultDSC:        nil,
			},
			wantErr: true,
			errMsg:  "LeaderElectionInitHooks.CreateDefaultDSC is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bootstrap.RunLeaderElectionInit(ctx, log, mockClient, cfg, tt.hooks)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRunLeaderElectionInit tests the main initialization function.
func TestRunLeaderElectionInit(t *testing.T) {
	ctx := context.Background()
	log := logr.Discard()
	mockClient := &MockClient{}

	tests := []struct {
		name          string
		cfg           bootstrap.LeaderElectionInitConfig
		setupMocks    func(*testing.T, *MockClient) bootstrap.LeaderElectionInitHooks
		expectedError string
		expectedLogs  []string
	}{
		{
			name: "successful initialization with all steps",
			cfg: bootstrap.LeaderElectionInitConfig{
				Platform:                  cluster.ManagedRhoai,
				ManifestsBasePath:         "/test/manifests",
				MonitoringNamespace:       "test-monitoring",
				DSCIEnabled:               true,
				DSCEnabled:                true,
				CreateDefaultDSCIDisabled: false,
			},
			setupMocks: func(t *testing.T, mc *MockClient) bootstrap.LeaderElectionInitHooks {
				t.Helper()
				return bootstrap.LeaderElectionInitHooks{
					CleanupExistingResource: func(ctx context.Context, cli client.Client, manifestsPath string) error {
						assert.Equal(t, "/test/manifests", manifestsPath)
						return nil
					},
					CreateDefaultDSCI: func(ctx context.Context, cli client.Client, platform common.Platform, monitoringNS string) error {
						assert.Equal(t, cluster.ManagedRhoai, platform)
						assert.Equal(t, "test-monitoring", monitoringNS)
						return nil
					},
					CreateDefaultDSC: func(ctx context.Context, cli client.Client) error {
						return nil
					},
				}
			},
			expectedError: "",
		},
		{
			name: "DSCI disabled",
			cfg: bootstrap.LeaderElectionInitConfig{
				Platform:                  cluster.SelfManagedRhoai,
				ManifestsBasePath:         "/test/manifests",
				MonitoringNamespace:       "test-monitoring",
				DSCIEnabled:               false,
				DSCEnabled:                true,
				CreateDefaultDSCIDisabled: false,
			},
			setupMocks: func(t *testing.T, mc *MockClient) bootstrap.LeaderElectionInitHooks {
				t.Helper()
				return bootstrap.LeaderElectionInitHooks{
					CleanupExistingResource: func(ctx context.Context, cli client.Client, manifestsPath string) error {
						return nil
					},
					CreateDefaultDSCI: func(ctx context.Context, cli client.Client, platform common.Platform, monitoringNS string) error {
						t.Error("CreateDefaultDSCI should not be called when DSCI is disabled")
						return nil
					},
					CreateDefaultDSC: func(ctx context.Context, cli client.Client) error {
						t.Error("CreateDefaultDSC should not be called for non-ManagedRhoai platform")
						return nil
					},
				}
			},
			expectedError: "",
		},
		{
			name: "DSCI auto-creation disabled",
			cfg: bootstrap.LeaderElectionInitConfig{
				Platform:                  cluster.OpenDataHub,
				ManifestsBasePath:         "/different/path",
				MonitoringNamespace:       "different-monitoring",
				DSCIEnabled:               true,
				DSCEnabled:                false,
				CreateDefaultDSCIDisabled: true,
			},
			setupMocks: func(t *testing.T, mc *MockClient) bootstrap.LeaderElectionInitHooks {
				t.Helper()
				return bootstrap.LeaderElectionInitHooks{
					CleanupExistingResource: func(ctx context.Context, cli client.Client, manifestsPath string) error {
						assert.Equal(t, "/different/path", manifestsPath)
						return nil
					},
					CreateDefaultDSCI: func(ctx context.Context, cli client.Client, platform common.Platform, monitoringNS string) error {
						t.Error("CreateDefaultDSCI should not be called when auto-creation is disabled")
						return nil
					},
					CreateDefaultDSC: func(ctx context.Context, cli client.Client) error {
						t.Error("CreateDefaultDSC should not be called when DSC is disabled")
						return nil
					},
				}
			},
			expectedError: "",
		},
		{
			name: "cleanup failure continues execution",
			cfg: bootstrap.LeaderElectionInitConfig{
				Platform:                  cluster.ManagedRhoai,
				ManifestsBasePath:         "/test/manifests",
				MonitoringNamespace:       "test-monitoring",
				DSCIEnabled:               true,
				DSCEnabled:                true,
				CreateDefaultDSCIDisabled: false,
			},
			setupMocks: func(t *testing.T, mc *MockClient) bootstrap.LeaderElectionInitHooks {
				t.Helper()
				return bootstrap.LeaderElectionInitHooks{
					CleanupExistingResource: func(ctx context.Context, cli client.Client, manifestsPath string) error {
						return errors.New("cleanup failed")
					},
					CreateDefaultDSCI: func(ctx context.Context, cli client.Client, platform common.Platform, monitoringNS string) error {
						// Should still be called even after cleanup failure
						return nil
					},
					CreateDefaultDSC: func(ctx context.Context, cli client.Client) error {
						return nil
					},
				}
			},
			expectedError: "", // No error returned despite cleanup failure
		},
		{
			name: "DSCI creation failure returns error",
			cfg: bootstrap.LeaderElectionInitConfig{
				Platform:                  cluster.ManagedRhoai,
				ManifestsBasePath:         "/test/manifests",
				MonitoringNamespace:       "test-monitoring",
				DSCIEnabled:               true,
				DSCEnabled:                true,
				CreateDefaultDSCIDisabled: false,
			},
			setupMocks: func(t *testing.T, mc *MockClient) bootstrap.LeaderElectionInitHooks {
				t.Helper()
				return bootstrap.LeaderElectionInitHooks{
					CleanupExistingResource: func(ctx context.Context, cli client.Client, manifestsPath string) error {
						return nil
					},
					CreateDefaultDSCI: func(ctx context.Context, cli client.Client, platform common.Platform, monitoringNS string) error {
						return errors.New("DSCI creation failed")
					},
					CreateDefaultDSC: func(ctx context.Context, cli client.Client) error {
						t.Error("CreateDefaultDSC should not be called if DSCI creation fails")
						return nil
					},
				}
			},
			expectedError: "create default DSCI: DSCI creation failed",
		},
		{
			name: "DSC creation failure returns error",
			cfg: bootstrap.LeaderElectionInitConfig{
				Platform:                  cluster.ManagedRhoai,
				ManifestsBasePath:         "/test/manifests",
				MonitoringNamespace:       "test-monitoring",
				DSCIEnabled:               true,
				DSCEnabled:                true,
				CreateDefaultDSCIDisabled: false,
			},
			setupMocks: func(t *testing.T, mc *MockClient) bootstrap.LeaderElectionInitHooks {
				t.Helper()
				return bootstrap.LeaderElectionInitHooks{
					CleanupExistingResource: func(ctx context.Context, cli client.Client, manifestsPath string) error {
						return nil
					},
					CreateDefaultDSCI: func(ctx context.Context, cli client.Client, platform common.Platform, monitoringNS string) error {
						return nil
					},
					CreateDefaultDSC: func(ctx context.Context, cli client.Client) error {
						return errors.New("DSC creation failed")
					},
				}
			},
			expectedError: "create default DSC: DSC creation failed",
		},
		{
			name: "DSC disabled for ManagedRhoai",
			cfg: bootstrap.LeaderElectionInitConfig{
				Platform:                  cluster.ManagedRhoai,
				ManifestsBasePath:         "/test/manifests",
				MonitoringNamespace:       "test-monitoring",
				DSCIEnabled:               true,
				DSCEnabled:                false,
				CreateDefaultDSCIDisabled: false,
			},
			setupMocks: func(t *testing.T, mc *MockClient) bootstrap.LeaderElectionInitHooks {
				t.Helper()
				return bootstrap.LeaderElectionInitHooks{
					CleanupExistingResource: func(ctx context.Context, cli client.Client, manifestsPath string) error {
						return nil
					},
					CreateDefaultDSCI: func(ctx context.Context, cli client.Client, platform common.Platform, monitoringNS string) error {
						return nil
					},
					CreateDefaultDSC: func(ctx context.Context, cli client.Client) error {
						t.Error("CreateDefaultDSC should not be called when DSC is disabled")
						return nil
					},
				}
			},
			expectedError: "",
		},
		{
			name: "invalid hooks returns error",
			cfg: bootstrap.LeaderElectionInitConfig{
				Platform:                  cluster.ManagedRhoai,
				ManifestsBasePath:         "/test/manifests",
				MonitoringNamespace:       "test-monitoring",
				DSCIEnabled:               true,
				DSCEnabled:                true,
				CreateDefaultDSCIDisabled: false,
			},
			setupMocks: func(t *testing.T, mc *MockClient) bootstrap.LeaderElectionInitHooks {
				t.Helper()
				return bootstrap.LeaderElectionInitHooks{
					CleanupExistingResource: nil, // Invalid hook
					CreateDefaultDSCI: func(ctx context.Context, cli client.Client, platform common.Platform, monitoringNS string) error {
						return nil
					},
					CreateDefaultDSC: func(ctx context.Context, cli client.Client) error { return nil },
				}
			},
			expectedError: "LeaderElectionInitHooks.CleanupExistingResource is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hooks := tt.setupMocks(t, mockClient)
			err := bootstrap.RunLeaderElectionInit(ctx, log, mockClient, tt.cfg, hooks)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRunLeaderElectionInitSequencing tests that operations execute in the correct order.
func TestRunLeaderElectionInitSequencing(t *testing.T) {
	ctx := context.Background()
	log := logr.Discard()
	mockClient := &MockClient{}

	var executionOrder []string

	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:                  cluster.ManagedRhoai,
		ManifestsBasePath:         "/test/manifests",
		MonitoringNamespace:       "test-monitoring",
		DSCIEnabled:               true,
		DSCEnabled:                true,
		CreateDefaultDSCIDisabled: false,
	}

	hooks := bootstrap.LeaderElectionInitHooks{
		CleanupExistingResource: func(ctx context.Context, cli client.Client, manifestsPath string) error {
			executionOrder = append(executionOrder, "cleanup")
			return nil
		},
		CreateDefaultDSCI: func(ctx context.Context, cli client.Client, platform common.Platform, monitoringNS string) error {
			executionOrder = append(executionOrder, "dsci")
			return nil
		},
		CreateDefaultDSC: func(ctx context.Context, cli client.Client) error {
			executionOrder = append(executionOrder, "dsc")
			return nil
		},
	}

	err := bootstrap.RunLeaderElectionInit(ctx, log, mockClient, cfg, hooks)
	require.NoError(t, err)

	// Verify the execution order: cleanup → DSCI → DSC
	expectedOrder := []string{"cleanup", "dsci", "dsc"}
	assert.Equal(t, expectedOrder, executionOrder, "Operations should execute in the correct sequence")
}

// TestLeaderElectionInitConfigValidation tests edge cases with configuration.
func TestLeaderElectionInitConfigValidation(t *testing.T) {
	ctx := context.Background()
	log := logr.Discard()
	mockClient := &MockClient{}

	// Test with empty manifests path
	cfg := bootstrap.LeaderElectionInitConfig{
		Platform:                  cluster.OpenDataHub,
		ManifestsBasePath:         "",
		MonitoringNamespace:       "monitoring",
		DSCIEnabled:               true,
		DSCEnabled:                false,
		CreateDefaultDSCIDisabled: false,
	}

	hooks := bootstrap.LeaderElectionInitHooks{
		CleanupExistingResource: func(ctx context.Context, cli client.Client, manifestsPath string) error {
			assert.Empty(t, manifestsPath, "Empty manifests path should be passed as-is")
			return nil
		},
		CreateDefaultDSCI: func(ctx context.Context, cli client.Client, platform common.Platform, monitoringNS string) error {
			assert.Equal(t, cluster.OpenDataHub, platform)
			assert.Equal(t, "monitoring", monitoringNS)
			return nil
		},
		CreateDefaultDSC: func(ctx context.Context, cli client.Client) error {
			t.Error("CreateDefaultDSC should not be called for OpenDataHub platform")
			return nil
		},
	}

	err := bootstrap.RunLeaderElectionInit(ctx, log, mockClient, cfg, hooks)
	require.NoError(t, err)
}

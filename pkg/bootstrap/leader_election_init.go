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

// Package bootstrap contains operator startup sequencing used after leader election.
package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/initialinstall"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

// LeaderElectionInitConfig carries flags/env-derived inputs for post-leader-election bootstrap.
// See RHOAIENG-48054 (cleanup must complete before default DSCI/DSC creation).
type LeaderElectionInitConfig struct {
	Platform                  common.Platform
	ManifestsBasePath         string
	MonitoringNamespace       string
	DSCIEnabled               bool
	DSCEnabled                bool
	CreateDefaultDSCIDisabled bool
}

// LeaderElectionInitHooks allows overriding install steps for tests.
type LeaderElectionInitHooks struct {
	CleanupExistingResource func(context.Context, client.Client, string) error
	CreateDefaultDSCI       func(context.Context, client.Client, common.Platform, string) error
	CreateDefaultDSC        func(context.Context, client.Client) error
}

// DefaultLeaderElectionInitHooks wires production implementations.
func DefaultLeaderElectionInitHooks() LeaderElectionInitHooks {
	return LeaderElectionInitHooks{
		CleanupExistingResource: upgrade.CleanupExistingResource,
		CreateDefaultDSCI:       initialinstall.CreateDefaultDSCI,
		CreateDefaultDSC:        initialinstall.CreateDefaultDSC,
	}
}

func validateLeaderElectionInitHooks(hooks LeaderElectionInitHooks) error {
	if hooks.CleanupExistingResource == nil {
		return errors.New("LeaderElectionInitHooks.CleanupExistingResource is nil")
	}
	if hooks.CreateDefaultDSCI == nil {
		return errors.New("LeaderElectionInitHooks.CreateDefaultDSCI is nil")
	}
	if hooks.CreateDefaultDSC == nil {
		return errors.New("LeaderElectionInitHooks.CreateDefaultDSC is nil")
	}
	return nil
}

// RunLeaderElectionInit performs post-leader-election initialization for the operator.
// It first runs cleanup logic, then creates default DSCI and DSC resources if enabled.
// Cleanup errors are logged but do not stop the process. If DSCI or DSC creation fails, the error is returned.
//
// Returns:
//
//	error - if DSCI or DSC creation fails, or hook validation fails
func RunLeaderElectionInit(ctx context.Context, log logr.Logger, cli client.Client, cfg LeaderElectionInitConfig, hooks LeaderElectionInitHooks) error {
	if err := validateLeaderElectionInitHooks(hooks); err != nil {
		return err
	}

	log.Info("run upgrade task")

	if err := hooks.CleanupExistingResource(ctx, cli, cfg.ManifestsBasePath); err != nil {
		// no error returned, continue with DSCI/DSC creation even if cleanup fails
		log.Error(err, "unable to cleanup existing resources")
	}

	switch {
	case !cfg.DSCIEnabled:
		log.Info("DSCI is disabled")
	case cfg.CreateDefaultDSCIDisabled:
		log.Info("default DSCI auto-creation is disabled")
	default:
		log.Info("create default DSCI CR")
		if err := hooks.CreateDefaultDSCI(ctx, cli, cfg.Platform, cfg.MonitoringNamespace); err != nil {
			log.Error(err, "unable to create default DSCI CR")
			return fmt.Errorf("create default DSCI: %w", err)
		}
	}

	if cfg.Platform == cluster.ManagedRhoai && cfg.DSCEnabled {
		log.Info("create default DSC")
		if err := hooks.CreateDefaultDSC(ctx, cli); err != nil {
			log.Error(err, "unable to create default DSC CR")
			return fmt.Errorf("create default DSC: %w", err)
		}
	}

	return nil
}

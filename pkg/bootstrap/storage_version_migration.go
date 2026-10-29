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

package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/storagemigration"
)

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions/status,verbs=patch,resourceNames=datascienceclusters.datasciencecluster.opendatahub.io
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions/status,verbs=patch,resourceNames=dscinitializations.dscinitialization.opendatahub.io
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions/status,verbs=patch,resourceNames=platforms.config.opendatahub.io
// +kubebuilder:rbac:groups=storagemigration.k8s.io;migration.k8s.io,resources=storageversionmigrations,verbs=get;create;delete
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// RunStorageVersionMigration registers independent leader runnables for DSC, DSCI, and Platform storage migration.
// Migration failures are logged; registration failures are collected and returned.
func RunStorageVersionMigration(mgr manager.Manager) error {
	recorder := mgr.GetEventRecorder(storagemigration.EventReporter)
	var errs error

	for _, crdName := range []string{
		gvk.DataScienceClusterCRDName,
		gvk.DSCInitializationCRDName,
		gvk.PlatformCRDName,
	} {
		// Ordinary RunnableFuncs belong to the manager's leader-election group.
		err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
			attempt, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			err := storagemigration.Migrate(attempt, mgr.GetConfig(), crdName, recorder)
			switch {
			case err == nil:
				return nil
			case ctx.Err() != nil:
				return nil
			}

			log.FromContext(ctx).Error(err, "storage version migration failed",
				"name", crdName,
				"namespace", "",
				"resourceKind", gvk.CustomResourceDefinition.Kind,
			)
			return nil
		}))
		if err != nil {
			errs = errors.Join(
				errs,
				fmt.Errorf("registering storage version migration for CRD %s: %w", crdName, err),
			)
		}
	}

	return errs
}

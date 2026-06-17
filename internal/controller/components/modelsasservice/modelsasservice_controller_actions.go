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

package modelsasservice

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const tenantCleanupFinalizer = "maas.opendatahub.io/tenant-cleanup"

func renderMaasOperatorInstall(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	if _, ok := rr.Instance.(*componentApi.ModelsAsService); !ok {
		return fmt.Errorf("resource instance is not ModelsAsService: %T", rr.Instance)
	}

	out, err := buildMaasOperatorInstallManifests(ctx, rr)
	if err != nil {
		return err
	}
	// Clear resources accumulated earlier in the pipeline so this action only applies the
	// maas-controller install bundle (see deploy.WithApplyOrder for apply ordering).
	rr.Resources = nil
	if err := rr.AddResources(out...); err != nil {
		return fmt.Errorf("add maas-controller install manifest: %w", err)
	}

	return nil
}

// stripTenantFinalizer is a finalizer action that removes the
// maas.opendatahub.io/tenant-cleanup finalizer from Tenant CRs before the
// ModelsAsService CR is deleted. Without this, Kubernetes owner-reference GC
// deletes the maas-controller Deployment (terminating the pod) before the
// Tenant finalizer can be processed, leaving the Tenant stuck in
// ConfigTerminating indefinitely.
func stripTenantFinalizer(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx)

	const pageSize = 200

	listOpts := []client.ListOption{
		client.InNamespace(MaaSSubscriptionNamespace),
		client.Limit(pageSize),
	}

	for {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk.Tenant)

		if err := rr.Client.List(ctx, list, listOpts...); err != nil {
			if meta.IsNoMatchError(err) {
				return nil
			}

			return fmt.Errorf("failed to list Tenant CRs: %w", err)
		}

		for i := range list.Items {
			item := &list.Items[i]

			if !controllerutil.RemoveFinalizer(item, tenantCleanupFinalizer) {
				continue
			}

			l.Info("stripping tenant-cleanup finalizer to unblock deletion",
				"name", item.GetName(),
				"namespace", item.GetNamespace(),
			)

			if err := rr.Client.Update(ctx, item); err != nil {
				if k8serr.IsNotFound(err) {
					continue
				}

				return fmt.Errorf("failed to strip finalizer from Tenant %s/%s: %w",
					item.GetNamespace(), item.GetName(), err)
			}
		}

		cont := list.GetContinue()
		if cont == "" {
			break
		}

		listOpts = []client.ListOption{
			client.InNamespace(MaaSSubscriptionNamespace),
			client.Limit(pageSize),
			client.Continue(cont),
		}
	}

	return nil
}

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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

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

// cleanupGatewayNamespaceResources deletes resources in the gateway namespace
// that have the MaaS component label but are no longer in the rendered manifests.
// Standard gc.NewAction() only covers the operator namespace; resources deployed
// cross-namespace (e.g. payload-processing in openshift-ingress) need explicit cleanup.
func cleanupGatewayNamespaceResources(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	if rr.SkipDeploy {
		return nil
	}

	l := log.FromContext(ctx)
	componentLabel := labels.ODH.Component(componentApi.ModelsAsServiceComponentName)

	// Build set of expected resources from the current rendered manifests.
	type objKey struct {
		gvk       schema.GroupVersionKind
		namespace string
		name      string
	}
	expected := make(map[objKey]bool)
	for i := range rr.Resources {
		res := &rr.Resources[i]
		if res.GetNamespace() == DefaultGatewayNamespace {
			expected[objKey{gvk: res.GroupVersionKind(), namespace: res.GetNamespace(), name: res.GetName()}] = true
		}
	}

	// GVKs to scan in the gateway namespace.
	gvks := []schema.GroupVersionKind{
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Version: "v1", Kind: "Service"},
		{Version: "v1", Kind: "ServiceAccount"},
		{Version: "v1", Kind: "ConfigMap"},
		{Group: "networking.k8s.io", Version: "v1", Kind: "NetworkPolicy"},
	}

	for _, gvk := range gvks {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		if err := rr.Client.List(ctx, list,
			client.InNamespace(DefaultGatewayNamespace),
			client.MatchingLabels{componentLabel: labels.True},
		); err != nil {
			return fmt.Errorf("list gateway namespace %s resources: %w", gvk.String(), err)
		}
		for i := range list.Items {
			item := &list.Items[i]
			k := objKey{gvk: item.GroupVersionKind(), namespace: item.GetNamespace(), name: item.GetName()}
			if expected[k] {
				continue
			}
			l.Info("deleting stale gateway namespace resource",
				"kind", item.GetKind(), "name", item.GetName(), "ns", item.GetNamespace())
			if err := rr.Client.Delete(ctx, item); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("delete stale %s %s/%s: %w", item.GetKind(), item.GetNamespace(), item.GetName(), err)
			}
		}
	}

	return nil
}

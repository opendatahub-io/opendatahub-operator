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
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func isCRDOrRESTMappingMiss(err error) bool {
	if k8serr.IsNotFound(err) || apimeta.IsNoMatchError(err) {
		return true
	}
	var nr *apimeta.NoResourceMatchError
	return errors.As(err, &nr)
}

// ensureMaasClusterConfigControllerRef sets the ModelsAsService CR as the controller owner on the
// cluster-scoped maas Config singleton once maas-controller has created it.
//
// We intentionally do not include Config in the operator kustomize bundle: maas-controller
// Lifecycle creates and continuously reconciles that CR (spec/status). Applying Config from the
// operator would fight that ownership of content while still needing this patch for ODH
// controller references, GC, and OwnsGVK watches, so only owner metadata is updated here via
// client Get/Patch after the object exists.
func ensureMaasClusterConfigControllerRef(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance is not ModelsAsService: %T", rr.Instance)
	}

	cfg := &unstructured.Unstructured{}
	cfg.SetGroupVersionKind(gvk.MaasConfig)
	key := client.ObjectKey{Name: MaasClusterConfigName}
	if err := rr.Client.Get(ctx, key, cfg); err != nil {
		if isCRDOrRESTMappingMiss(err) || k8serr.IsNotFound(err) {
			logf.FromContext(ctx).V(2).Info(
				"skipping maas Config controller reference: Config not available yet or API missing",
				"name", MaasClusterConfigName,
			)
			return nil
		}
		return fmt.Errorf("get maas cluster Config %q: %w", MaasClusterConfigName, err)
	}

	desired := cfg.DeepCopy()
	if err := ctrlutil.SetControllerReference(mas, desired, rr.Client.Scheme()); err != nil {
		return fmt.Errorf("maas Config %s controller reference: %w", MaasClusterConfigName, err)
	}

	if equality.Semantic.DeepEqual(cfg.GetOwnerReferences(), desired.GetOwnerReferences()) {
		return nil
	}

	if err := rr.Client.Patch(ctx, desired, client.MergeFrom(cfg)); err != nil {
		return fmt.Errorf("patch maas Config %s ownerReferences: %w", MaasClusterConfigName, err)
	}

	return nil
}

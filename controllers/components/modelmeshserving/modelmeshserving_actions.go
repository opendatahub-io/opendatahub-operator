/*
Copyright 2023.

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

package modelmeshserving

import (
	"context"
	"fmt"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// early exit
	_, ok := rr.Instance.(*componentsv1.ModelMeshServing)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentsv1.ModelMeshServing)", rr.Instance)
	}
	// setup Manifets[0] for modelmeshserving
	rr.Manifests = append(rr.Manifests, odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "overlays/odh",
	})
	return nil
}

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mm, ok := rr.Instance.(*componentsv1.ModelMeshServing)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentsv1.ModelMeshServing)", rr.Instance)
	}
	df := mm.GetDevFlags()
	if df == nil {
		return nil
	}
	if len(df.Manifests) == 0 {
		return nil
	}

	// Implement devflags support logic
	// If dev flags are set, update default manifests path
	for _, subcomponent := range df.Manifests {
		if strings.Contains(subcomponent.URI, ComponentName) {
			// Download modelmeshserving
			if err := odhdeploy.DownloadManifests(ctx, ComponentName, subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			if subcomponent.SourcePath != "" {
				rr.Manifests[0].SourcePath = subcomponent.SourcePath
			}
		}
	}
	// TODO: Implement devflags logmode logic
	return nil
}

func patchOwnerReference(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mm, ok := rr.Instance.(*componentsv1.ModelMeshServing)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentsv1.ModelMeshServing)", rr.Instance)
	}

	l := logf.FromContext(ctx)
	mc := &componentsv1.ModelController{}
	if err := rr.Client.Get(ctx, client.ObjectKey{Name: componentsv1.ModelControllerInstanceName}, mc); err != nil {
		if k8serr.IsNotFound(err) {
			return odherrors.NewStopError("ModelController CR not exist: %v", err)
		}
		return odherrors.NewStopError("failed to get ModelController CR: %v", err)
	}
	l.Info("Get WEN CR", "ModelController", mc)
	for _, owners := range mc.GetOwnerReferences() {
		if owners.UID == mm.GetUID() {
			return nil // modelmesh already as owner to modelcontroller, early exit
		}
	}

	owners := []metav1.OwnerReference{}
	for _, o := range mc.GetOwnerReferences() {
		if o.UID == mm.GetUID() {
			return nil // same modelmesh already as owner to modelcontroller, early exit
		}
		if o.Kind != componentsv1.ModelMeshServingKind { // TODO: a workaround to ensure no old UID exist, this should be moved into finalizer later
			owners = append(owners, o)
		}
	}
	owners = append(owners, metav1.OwnerReference{
		Kind:               componentsv1.ModelMeshServingKind,
		APIVersion:         componentsv1.GroupVersion.String(),
		Name:               componentsv1.ModelMeshServingInstanceName,
		UID:                mm.GetUID(),
		BlockOwnerDeletion: ptr.To(false),
		Controller:         ptr.To(false),
	},
	)
	mc.SetOwnerReferences(owners)
	mc.SetManagedFields(nil) // remove managed fields to avoid conflicts when SSA apply
	opt := &client.PatchOptions{
		Force:        ptr.To(true),
		FieldManager: componentsv1.ModelControllerInstanceName,
	}
	if err := rr.Client.Patch(ctx, mc, client.Apply, opt); err != nil {
		return fmt.Errorf("error update ownerreference for CR %s : %w", mc.GetName(), err)
	}
	l.Info("Update Ownerreference on modelmesh change")
	return nil
}

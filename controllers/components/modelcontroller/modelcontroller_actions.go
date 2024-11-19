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

package modelcontroller

import (
	"context"
	"fmt"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// early exist
	_, ok := rr.Instance.(*componentsv1.ModelController)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentsv1.ModelController)", rr.Instance)
	}
	rr.Manifests = append(rr.Manifests, odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "base",
	})
	return nil
}

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	_, ok := rr.Instance.(*componentsv1.ModelController)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentsv1.ModelController)", rr.Instance)
	}
	// Get Kserve which can override Kserve devflags
	k := rr.DSC.Spec.Components.Kserve
	if k.ManagementSpec.ManagementState != operatorv1.Managed || k.DevFlags == nil || len(k.DevFlags.Manifests) == 0 {
		// Get ModelMeshServing if it is enabled and has devlfags
		mm := rr.DSC.Spec.Components.ModelMeshServing
		if mm.ManagementSpec.ManagementState != operatorv1.Managed || mm.DevFlags == nil || len(mm.DevFlags.Manifests) == 0 {
			return nil
		}

		for _, subcomponent := range rr.DSC.Spec.Components.ModelMeshServing.DevFlags.Manifests {
			if strings.Contains(subcomponent.URI, ComponentName) {
				// Download odh-model-controller
				if err := odhdeploy.DownloadManifests(ctx, ComponentName, subcomponent); err != nil {
					return err
				}
				// If overlay is defined, update paths
				if subcomponent.SourcePath != "" {
					rr.Manifests[0].SourcePath = subcomponent.SourcePath
				}
			}
		}
		return nil
	}

	for _, subcomponent := range rr.DSC.Spec.Components.Kserve.DevFlags.Manifests {
		if strings.Contains(subcomponent.URI, ComponentName) {
			// Download odh-model-controller
			if err := odhdeploy.DownloadManifests(ctx, ComponentName, subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			if subcomponent.SourcePath != "" {
				rr.Manifests[0].SourcePath = subcomponent.SourcePath
			}
		}
	}
	return nil
}

func patchOwnerReference(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := log.FromContext(ctx)
	mc, ok := rr.Instance.(*componentsv1.ModelController)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentsv1.ModelController)", rr.Instance)
	}

	owners := []metav1.OwnerReference{}
	if mc.Spec.ModelMeshServing == operatorv1.Managed { // when modelmesh is Removed, it wont be set in the owners in next update
		mm := &componentsv1.ModelMeshServing{}
		if err := rr.Client.Get(ctx, client.ObjectKey{Name: "default-modelmesh"}, mm); err != nil {
			if k8serr.IsNotFound(err) {
				return odherrors.NewStopError("ModelMesh CR not exist: %v", err)
			}
			return odherrors.NewStopError("failed to get ModelMesh CR: %v", err)
		}
		owners = append(owners, metav1.OwnerReference{
			Kind:               componentsv1.ModelMeshServingKind,
			APIVersion:         componentsv1.GroupVersion.String(),
			Name:               componentsv1.ModelMeshServingInstanceName,
			UID:                mm.GetUID(),
			BlockOwnerDeletion: func(b bool) *bool { return &b }(false),
			Controller:         func(b bool) *bool { return &b }(false),
		},
		)
		l.Info("Update Ownerreference on modelmesh change")
	}
	if mc.Spec.Kserve == operatorv1.Managed { // when kserve is Removed, it wont be set in the owners in next update
		k := &componentsv1.Kserve{}
		if err := rr.Client.Get(ctx, client.ObjectKey{Name: "default-kserve"}, k); err != nil {
			if k8serr.IsNotFound(err) {
				return odherrors.NewStopError("Kserve CR not exist: %v", err)
			}
			return odherrors.NewStopError("failed to get Kserve CR: %v", err)
		}
		owners = append(owners, metav1.OwnerReference{
			Kind:               componentsv1.KserveKind,
			APIVersion:         componentsv1.GroupVersion.String(),
			Name:               componentsv1.KserveInstanceName,
			UID:                k.GetUID(),
			BlockOwnerDeletion: func(b bool) *bool { return &b }(false),
			Controller:         func(b bool) *bool { return &b }(false),
		},
		)
		l.Info("Update Ownerreference on kserve change")
	}

	mc.SetOwnerReferences(owners)
	mc.SetManagedFields(nil) // remove managed fields to avoid conflicts when SSA apply
	force := true
	opt := &client.PatchOptions{
		Force:        &force,
		FieldManager: componentsv1.ModelControllerInstanceName,
	}
	if err := rr.Client.Patch(ctx, mc, client.Apply, opt); err != nil {
		return fmt.Errorf("error update ownerreference for CR %s : %w", mc.GetName(), err)
	}
	return nil
}

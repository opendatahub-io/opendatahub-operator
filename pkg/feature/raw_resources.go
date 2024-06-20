/*
Copyright (c) 2016-2017 Bitnami
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

package feature

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	YamlSeparator = "(?m)^---[ \t]*$"
)

func applyResources(ctx context.Context, cli client.Client, objects []*unstructured.Unstructured, metaOptions ...cluster.MetaOptions) error {
	for _, object := range objects {
		for _, opt := range metaOptions {
			if err := opt(object); err != nil {
				return err
			}
		}

		name := object.GetName()
		namespace := object.GetNamespace()

		err := cli.Get(ctx, k8stypes.NamespacedName{Name: name, Namespace: namespace}, object.DeepCopy())
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to get object %s/%s: %w", namespace, name, err)
		}

		if err != nil {
			// object does not exist and should be created
			if createErr := cli.Create(ctx, object); client.IgnoreAlreadyExists(createErr) != nil {
				return fmt.Errorf("failed to create object %s/%s: %w", namespace, name, createErr)
			}
		}
		// object exists, check if it is managed
		isManaged, isAnnotated := object.GetAnnotations()[annotations.ManagedByODHOperator]
		if isAnnotated && isManaged == "true" {
			// update the object since we manage it
			if updateErr := cli.Update(ctx, object); updateErr != nil {
				return fmt.Errorf("failed to update object %s/%s: %w", namespace, name, updateErr)
			}
		}
		// object exists and is not manged, skip reconcile allowing users to tweak it
	}
	return nil
}

func patchResources(ctx context.Context, cli client.Client, patches []*unstructured.Unstructured) error {
	for _, patch := range patches {
		// Convert the individual resource patch to JSON
		patchAsJSON, err := patch.MarshalJSON()
		if err != nil {
			return fmt.Errorf("error converting yaml to json: %w", err)
		}

		if err = cli.Patch(ctx, patch, client.RawPatch(k8stypes.MergePatchType, patchAsJSON)); err != nil {
			return fmt.Errorf("failed patching resource: %w", err)
		}
	}

	return nil
}

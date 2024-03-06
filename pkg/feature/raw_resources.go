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

	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

const (
	YamlSeparator = "(?m)^---[ \t]*$"
)

func createResources(cli client.Client, objects []*unstructured.Unstructured, metaOptions ...cluster.MetaOptions) error {
	for _, object := range objects {
		for _, opt := range metaOptions {
			if err := opt(object); err != nil {
				return err // return immediately if any of the MetaOptions functions fail
			}
		}

		name := object.GetName()
		namespace := object.GetNamespace()

		err := cli.Get(context.TODO(), k8stypes.NamespacedName{Name: name, Namespace: namespace}, object.DeepCopy())
		if err == nil {
			// object already exists, skip reconcile allowing users to tweak it
			continue
		}
		if !k8serrors.IsNotFound(err) {
			return errors.WithStack(err)
		}

		err = cli.Create(context.TODO(), object)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

func patchResources(cli client.Client, patches []*unstructured.Unstructured) error {
	for _, patch := range patches {
		// Convert the individual resource patch to JSON
		patchAsJSON, err := patch.MarshalJSON()
		if err != nil {
			return fmt.Errorf("error converting yaml to json: %w", err)
		}

		if err = cli.Patch(context.TODO(), patch, client.RawPatch(k8stypes.MergePatchType, patchAsJSON)); err != nil {
			return fmt.Errorf("failed patching resource: %w", err)
		}
	}

	return nil
}

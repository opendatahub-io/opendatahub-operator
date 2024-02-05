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
	"strings"

	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
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

		if !isNamespaceSet(object) {
			return fmt.Errorf("no NS is set on %s", object.GetName())
		}

		name := object.GetName()
		namespace := object.GetNamespace()

		err := cli.Get(context.TODO(), k8stypes.NamespacedName{Name: name, Namespace: namespace}, object.DeepCopy())
		if err == nil {
			// object already exists
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

func patchResources(dyCli dynamic.Interface, patches []*unstructured.Unstructured) error {
	for _, patch := range patches {
		gvr := schema.GroupVersionResource{
			Group:    strings.ToLower(patch.GroupVersionKind().Group),
			Version:  patch.GroupVersionKind().Version,
			Resource: strings.ToLower(patch.GroupVersionKind().Kind) + "s",
		}

		// Convert the individual resource patch to JSON
		patchAsJSON, err := patch.MarshalJSON() // todo: ensure if this is the right method for the task
		if err != nil {
			return errors.WithStack(err)
		}

		_, err = dyCli.Resource(gvr).
			Namespace(patch.GetNamespace()).
			Patch(context.TODO(), patch.GetName(), k8stypes.MergePatchType, patchAsJSON, metav1.PatchOptions{})
		if err != nil {
			return errors.WithStack(err)
		}

		if err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func isNamespaceSet(u *unstructured.Unstructured) bool {
	namespace := u.GetNamespace()
	if u.GetKind() != "Namespace" && namespace == "" {
		return false
	}
	return true
}

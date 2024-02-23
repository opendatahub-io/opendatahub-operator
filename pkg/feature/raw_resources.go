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
	"regexp"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	YamlSeparator = "(?m)^---[ \t]*$"
)

func (f *Feature) createResources(resources string) error {
	splitter := regexp.MustCompile(YamlSeparator)
	objectStrings := splitter.Split(resources, -1)
	for _, str := range objectStrings {
		if strings.TrimSpace(str) == "" {
			continue
		}
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(str), u); err != nil {
			return errors.WithStack(err)
		}

		ensureNamespaceIsSet(f, u)

		name := u.GetName()
		namespace := u.GetNamespace()

		u.SetOwnerReferences([]metav1.OwnerReference{
			f.AsOwnerReference(),
		})

		f.Log.Info("Creating resource", "name", name)

		err := f.Client.Get(context.TODO(), k8stypes.NamespacedName{Name: name, Namespace: namespace}, u.DeepCopy())
		if err == nil {
			f.Log.Info("Object already exists...")

			continue
		}
		if !k8serrors.IsNotFound(err) {
			return errors.WithStack(err)
		}

		err = f.Client.Create(context.TODO(), u)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

func (f *Feature) patchResources(resources string) error {
	splitter := regexp.MustCompile(YamlSeparator)
	objectStrings := splitter.Split(resources, -1)
	for _, str := range objectStrings {
		if strings.TrimSpace(str) == "" {
			continue
		}
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(str), u); err != nil {
			f.Log.Error(err, "error unmarshalling yaml")
			return errors.WithStack(err)
		}

		ensureNamespaceIsSet(f, u)

		patchAsJSON, err := yaml.YAMLToJSON([]byte(str))
		if err != nil {
			return fmt.Errorf("error converting yaml to json: %w", err)
		}

		if err := f.Client.Patch(context.TODO(), u, client.RawPatch(k8stypes.MergePatchType, patchAsJSON)); err != nil {
			return fmt.Errorf("failed patching resource: %w", err)
		}
	}

	return nil
}

// For any other than Namespace kind we set namespace to AppNamespace if it is not defined
// yet for the object.
func ensureNamespaceIsSet(f *Feature, u *unstructured.Unstructured) {
	namespace := u.GetNamespace()
	if u.GetKind() != "Namespace" && namespace == "" {
		u.SetNamespace(f.Spec.AppNamespace)
	}
}

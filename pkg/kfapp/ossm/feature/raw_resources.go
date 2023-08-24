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
	"github.com/ghodss/yaml"
	configtypes "github.com/opendatahub-io/opendatahub-operator/apis/config"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"os"
	"regexp"
	"strings"
)

const (
	YamlSeparator = "(?m)^---[ \t]*$"
)

func (f *Feature) createResourceFromFile(filename string, elems ...configtypes.NameValue) error {
	elemsMap := make(map[string]configtypes.NameValue)
	for _, nv := range elems {
		elemsMap[nv.Name] = nv
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return errors.WithStack(err)
	}
	splitter := regexp.MustCompile(YamlSeparator)
	objectStrings := splitter.Split(string(data), -1)
	for _, str := range objectStrings {
		if strings.TrimSpace(str) == "" {
			continue
		}
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(str), u); err != nil {
			return errors.WithStack(err)
		}

		name := u.GetName()
		namespace := u.GetNamespace()
		if namespace == "" {
			if val, exists := elemsMap["namespace"]; exists {
				u.SetNamespace(val.Value)
			} else {
				u.SetNamespace("default")
			}
		}

		u.SetOwnerReferences([]metav1.OwnerReference{
			f.OwnerReference(),
		})

		logrus.Infof("Creating %s", name)

		err := f.client.Get(context.TODO(), k8stypes.NamespacedName{Name: name, Namespace: namespace}, u.DeepCopy())
		if err == nil {
			log.Info("Object already exists...")
			continue
		}
		if !k8serrors.IsNotFound(err) {
			return errors.WithStack(err)
		}

		err = f.client.Create(context.TODO(), u)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

func (f *Feature) patchResourceFromFile(filename string, elems ...configtypes.NameValue) error {
	elemsMap := make(map[string]configtypes.NameValue)
	for _, nv := range elems {
		elemsMap[nv.Name] = nv
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return errors.WithStack(err)
	}
	splitter := regexp.MustCompile(YamlSeparator)
	objectStrings := splitter.Split(string(data), -1)
	for _, str := range objectStrings {
		if strings.TrimSpace(str) == "" {
			continue
		}
		p := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(str), p); err != nil {
			logrus.Error("error unmarshalling yaml")
			return errors.WithStack(err)
		}

		// Adding `namespace:` to Namespace resource doesn't make sense
		if p.GetKind() != "Namespace" {
			namespace := p.GetNamespace()
			if namespace == "" {
				if val, exists := elemsMap["namespace"]; exists {
					p.SetNamespace(val.Value)
				} else {
					p.SetNamespace("default")
				}
			}
		}

		gvr := schema.GroupVersionResource{
			Group:    strings.ToLower(p.GroupVersionKind().Group),
			Version:  p.GroupVersionKind().Version,
			Resource: strings.ToLower(p.GroupVersionKind().Kind) + "s",
		}

		// Convert the patch from YAML to JSON
		patchAsJson, err := yaml.YAMLToJSON(data)
		if err != nil {
			logrus.Error("error converting yaml to json")
			return errors.WithStack(err)
		}

		_, err = f.dynamicClient.Resource(gvr).
			Namespace(p.GetNamespace()).
			Patch(context.Background(), p.GetName(), k8stypes.MergePatchType, patchAsJson, metav1.PatchOptions{})
		if err != nil {
			logrus.Error("error patching resource\n",
				fmt.Sprintf("%+v\n", gvr),
				fmt.Sprintf("%+v\n", p),
				fmt.Sprintf("%+v\n", patchAsJson))
			return errors.WithStack(err)
		}

		if err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

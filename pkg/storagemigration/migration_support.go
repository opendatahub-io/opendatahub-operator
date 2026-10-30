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

package storagemigration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	requestKind        = "StorageVersionMigration"
	annotationPrefix   = "storage-migration.opendatahub.io/"
	succeededCondition = "Succeeded"
)

var migrationAPIs = []schema.GroupVersionResource{
	{
		Group:    "storagemigration.k8s.io",
		Version:  "v1",
		Resource: "storageversionmigrations",
	},
	{
		Group:    "storagemigration.k8s.io",
		Version:  "v1beta1",
		Resource: "storageversionmigrations",
	},
	{
		Group:    "storagemigration.k8s.io",
		Version:  "v1alpha1",
		Resource: "storageversionmigrations",
	},
	{
		Group:    "migration.k8s.io",
		Version:  "v1alpha1",
		Resource: "storageversionmigrations",
	},
}

// Migrate validates, discovers, runs, and cleans up storage migration for one CRD.
// Each executed step emits an outcome event. The caller supplies the attempt deadline.
func Migrate(ctx context.Context, config *rest.Config, crdName string, recorder events.EventRecorder) error {
	if recorder == nil {
		return errors.New("migration event recorder is nil")
	}

	if config == nil {
		return errors.New("kubernetes REST config is nil")
	}

	definitions, err := apiextensions.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("constructing CRD client: %w", err)
	}

	requests, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("constructing migration client: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return fmt.Errorf("constructing discovery client: %w", err)
	}

	m := migration{
		crdName:     crdName,
		definitions: definitions.CustomResourceDefinitions(),
		requests:    requests,
		discovery:   discoveryClient,
		recorder:    recorder,
		interval:    time.Second,
	}

	if err := m.execute(ctx); err != nil {
		return err
	}

	return nil
}

func discoverMigrationAPI(_ context.Context, discoveryClient discovery.ServerResourcesInterface) (schema.GroupVersionResource, error) {
	for _, api := range migrationAPIs {
		resources, err := discoveryClient.ServerResourcesForGroupVersion(api.GroupVersion().String())
		switch {
		case k8serr.IsNotFound(err):
			continue
		case err != nil:
			return schema.GroupVersionResource{}, fmt.Errorf("discovering %s: %w", api.GroupVersion(), err)
		case resources == nil:
			return schema.GroupVersionResource{}, fmt.Errorf("empty discovery response for %s", api.GroupVersion())
		}

		for _, resource := range resources.APIResources {
			switch {
			case resource.Name != api.Resource:
				continue
			case resource.Namespaced:
				return schema.GroupVersionResource{}, fmt.Errorf("migration API %s must be cluster-scoped", api.GroupVersion())
			case resource.Kind != requestKind:
				return schema.GroupVersionResource{}, fmt.Errorf("unexpected migration kind %q for %s", resource.Kind, api.GroupVersion())
			case resources.GroupVersion != api.GroupVersion().String():
				return schema.GroupVersionResource{}, fmt.Errorf("unexpected discovery version %q for %s", resources.GroupVersion, api.GroupVersion())
			default:
				return api, nil
			}
		}
	}

	return schema.GroupVersionResource{}, errors.New(
		"no supported StorageVersionMigration API; enable the cluster storage migration API and controller before restarting the operator",
	)
}

func newRequest(crd *apiextensionsv1.CustomResourceDefinition, storage string, api schema.GroupVersionResource) (*unstructured.Unstructured, error) {
	identity := fmt.Sprintf("%s/%s/%d/%s", crd.Name, crd.UID, crd.Generation, storage)
	digest := sha256.Sum256([]byte(identity))

	resource := map[string]any{
		"group":    crd.Spec.Group,
		"resource": crd.Spec.Names.Plural,
	}
	if api.Version == "v1alpha1" {
		resource["version"] = storage
	}

	request, err := resources.ToUnstructured(&map[string]any{
		"apiVersion": api.GroupVersion().String(),
		"kind":       requestKind,
		"spec":       map[string]any{"resource": resource},
	})
	if err != nil {
		return nil, fmt.Errorf("converting migration request: %w", err)
	}

	request.SetName(fmt.Sprintf("odh-storage-%x", digest[:20]))
	resources.SetLabel(request, "app.kubernetes.io/managed-by", "opendatahub-operator")
	resources.SetAnnotation(request, annotationPrefix+"crd-uid", string(crd.UID))
	resources.SetAnnotation(request, annotationPrefix+"crd-generation", strconv.FormatInt(crd.Generation, 10))
	resources.SetAnnotation(request, annotationPrefix+"storage-version", storage)

	return request, nil
}

func createOrGetMigrationRequest(ctx context.Context, requests dynamic.ResourceInterface, expected *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	request, err := requests.Create(ctx, expected, metav1.CreateOptions{})
	if k8serr.IsAlreadyExists(err) {
		request, err = requests.Get(ctx, expected.GetName(), metav1.GetOptions{})
	}

	if err != nil {
		return nil, fmt.Errorf("creating/resuming %s request %s: %w", expected.GetAPIVersion(), expected.GetName(), err)
	}

	return request, nil
}

func validateRequest(request *unstructured.Unstructured, expected *unstructured.Unstructured) error {
	switch {
	case request == nil:
		return errors.New("migration request is nil")
	case expected.GetUID() != "" && request.GetUID() != expected.GetUID():
		return fmt.Errorf("migration request %s was replaced", request.GetName())
	case request.GetUID() == "" || request.GetDeletionTimestamp() != nil:
		return fmt.Errorf("migration request %s has no usable identity or is deleting", request.GetName())
	case request.GetName() != expected.GetName() || request.GetKind() != requestKind || request.GetAPIVersion() != expected.GetAPIVersion():
		return fmt.Errorf("migration request %s has unexpected identity", request.GetName())
	case resources.GetLabel(request, "app.kubernetes.io/managed-by") != "opendatahub-operator":
		return fmt.Errorf("migration request %s is not owned by this operator", request.GetName())
	}

	for key, value := range expected.GetAnnotations() {
		if resources.GetAnnotation(request, key) != value {
			return fmt.Errorf("migration request %s has mismatched %s; review it before retrying", request.GetName(), key)
		}
	}

	if !reflect.DeepEqual(request.Object["spec"], expected.Object["spec"]) {
		return fmt.Errorf("migration request %s has unexpected spec", request.GetName())
	}

	return nil
}

func requestSucceeded(request *unstructured.Unstructured) (bool, error) {
	var state struct {
		Status struct {
			Conditions []metav1.Condition `json:"conditions"`
		} `json:"status"`
	}

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(request.Object, &state); err != nil {
		return false, fmt.Errorf("decoding migration conditions: %w", err)
	}

	complete := false
	for _, condition := range state.Status.Conditions {
		switch condition.Status {
		case metav1.ConditionFalse:
			continue
		case metav1.ConditionUnknown:
			continue
		case metav1.ConditionTrue:
			switch condition.Type {
			case "Failed":
				return false, fmt.Errorf(
					"migration request %s failed: %s: %s; review and manually delete the failed request before restarting the operator",
					request.GetName(), condition.Reason, condition.Message,
				)
			case succeededCondition:
				complete = true
			}
		default:
			return false, errors.New("invalid migration condition status")
		}
	}

	return complete, nil
}

func storageVersion(crd *apiextensionsv1.CustomResourceDefinition) (string, error) {
	switch {
	case crd == nil:
		return "", errors.New("CRD is nil")
	case crd.UID == "" || crd.ResourceVersion == "":
		return "", errors.New("CRD has no usable UID or resourceVersion")
	case crd.DeletionTimestamp != nil:
		return "", errors.New("CRD is deleting")
	case crd.Spec.Group == "" || crd.Spec.Names.Plural == "" || crd.Name != crd.Spec.Names.Plural+"."+crd.Spec.Group:
		return "", errors.New("CRD has invalid group/resource identity")
	}

	storage := ""
	for _, version := range crd.Spec.Versions {
		if !version.Storage {
			continue
		}

		if storage != "" || version.Name == "" || !version.Served {
			return "", errors.New("CRD must have exactly one nonempty served storage version")
		}

		storage = version.Name
	}

	if storage == "" || !slices.Contains(crd.Status.StoredVersions, storage) {
		return "", errors.New("CRD storage history must contain its storage version")
	}

	seen := sets.New[string]()
	for _, version := range crd.Status.StoredVersions {
		if version == "" || seen.Has(version) {
			return "", errors.New("CRD storage history has empty or duplicate versions")
		}
		seen.Insert(version)
	}

	return storage, nil
}

func pruneStorageHistory(
	ctx context.Context,
	definitions apiextensions.CustomResourceDefinitionInterface,
	original *apiextensionsv1.CustomResourceDefinition,
	storage string,
) error {
	current, err := definitions.Get(ctx, original.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("rereading CRD: %w", err)
	}

	currentStorage, err := storageVersion(current)
	switch {
	case err != nil:
		return err
	case current.UID != original.UID ||
		currentStorage != storage:
		return errors.New("CRD identity or storage configuration changed during migration")
	case current.Generation != original.Generation:
		return errors.New("CRD generation changed during migration")
	case slices.Equal(current.Status.StoredVersions, []string{storage}):
		return nil
	}

	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"resourceVersion": current.ResourceVersion},
		"status":   map[string]any{"storedVersions": []string{storage}},
	})
	if err != nil {
		return fmt.Errorf("encoding storage history patch: %w", err)
	}

	_, err = definitions.Patch(ctx, current.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "status")
	if err != nil {
		return fmt.Errorf("patching CRD storage history: %w", err)
	}

	return nil
}

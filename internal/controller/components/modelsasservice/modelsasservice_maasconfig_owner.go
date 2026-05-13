/*
Copyright 2025.

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
	"encoding/json"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apimachinerylabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhlabels "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const maasConfigPluralResource = "configs"

func isCRDOrRESTMappingMiss(err error) bool {
	if k8serr.IsNotFound(err) || apimeta.IsNoMatchError(err) {
		return true
	}
	var nr *apimeta.NoResourceMatchError
	return errors.As(err, &nr)
}

func maasConfigGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    gvk.MaasConfig.Group,
		Version:  gvk.MaasConfig.Version,
		Resource: maasConfigPluralResource,
	}
}

// selectMaasClusterConfig picks the cluster-scoped maas Config CR to attach the ModelsAsService
// controller reference to. Prefer objects labeled like the operator-managed maas-controller bundle;
// if none match, accept a single unlabeled singleton.
func selectMaasClusterConfig(objs []unstructured.Unstructured) *unstructured.Unstructured {
	partOfKey := odhlabels.K8SCommon.PartOf
	partOfVal := componentApi.ModelsAsServiceComponentName
	compKey := odhlabels.ODH.Component(componentApi.ModelsAsServiceComponentName)

	var partOfMatches []unstructured.Unstructured
	for i := range objs {
		if objs[i].GetLabels()[partOfKey] == partOfVal {
			partOfMatches = append(partOfMatches, objs[i])
		}
	}

	switch len(partOfMatches) {
	case 0:
		if len(objs) == 1 {
			return &objs[0]
		}
		return nil
	case 1:
		return &partOfMatches[0]
	default:
		for i := range partOfMatches {
			if partOfMatches[i].GetLabels()[compKey] == odhlabels.True {
				return &partOfMatches[i]
			}
		}
		return &partOfMatches[0]
	}
}

// ensureMaasClusterConfigControllerRef sets the ModelsAsService CR as the controller owner on the
// cluster maas Config once maas-controller has created it. This keeps OwnsGVK(MaasConfig) watches
// and component GC aligned with runtime-created Config objects.
func ensureMaasClusterConfigControllerRef(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance is not ModelsAsService: %T", rr.Instance)
	}

	dc := rr.Controller.GetDynamicClient()
	if dc == nil {
		return errors.New("dynamic client is nil; cannot reconcile maas Config ownership")
	}

	gvr := maasConfigGVR()
	partOfSel := apimachinerylabels.Set{
		odhlabels.K8SCommon.PartOf: componentApi.ModelsAsServiceComponentName,
	}.AsSelector().String()

	list, err := dc.Resource(gvr).Namespace("").List(ctx, metav1.ListOptions{LabelSelector: partOfSel})
	if err != nil {
		if isCRDOrRESTMappingMiss(err) {
			return nil
		}
		return fmt.Errorf("list maas Config CRs (labeled): %w", err)
	}

	if len(list.Items) == 0 {
		list, err = dc.Resource(gvr).Namespace("").List(ctx, metav1.ListOptions{})
		if err != nil {
			if isCRDOrRESTMappingMiss(err) {
				return nil
			}
			return fmt.Errorf("list maas Config CRs (unfiltered): %w", err)
		}
	}

	chosen := selectMaasClusterConfig(list.Items)
	if chosen == nil {
		logf.FromContext(ctx).V(2).Info(
			"skipping maas Config controller reference: no suitable Config object (maas-controller may not have created it yet)",
			"configCount", len(list.Items),
		)
		return nil
	}

	desired := chosen.DeepCopy()
	if err := ctrlutil.SetControllerReference(mas, desired, rr.Client.Scheme()); err != nil {
		return fmt.Errorf("maas Config %s controller reference: %w", chosen.GetName(), err)
	}

	if equality.Semantic.DeepEqual(chosen.GetOwnerReferences(), desired.GetOwnerReferences()) {
		return nil
	}

	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"ownerReferences": desired.GetOwnerReferences(),
			"resourceVersion": chosen.GetResourceVersion(),
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal maas Config owner patch: %w", err)
	}

	_, err = dc.Resource(gvr).Namespace(chosen.GetNamespace()).Patch(
		ctx,
		chosen.GetName(),
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patch maas Config %s ownerReferences: %w", chosen.GetName(), err)
	}

	return nil
}

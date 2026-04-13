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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

// MaaSTenantPlatform wraps *maasv1alpha1.MaaSTenant so it satisfies common.PlatformObject for DSC
// materialization while the API object persisted on the cluster remains MaaSTenant.
//
// APIPersistObject implements persistence delegation for callers that need the inner API type
// (e.g. AddResources / server-side apply) because the wrapper type is not registered in the scheme.
type MaaSTenantPlatform struct {
	*maasv1alpha1.MaaSTenant

	bridge common.Status
}

var _ common.PlatformObject = (*MaaSTenantPlatform)(nil)

// NewMaaSTenantPlatform returns a wrapper with a non-nil inner tenant (useful for tests).
func NewMaaSTenantPlatform() *MaaSTenantPlatform {
	return &MaaSTenantPlatform{
		MaaSTenant: &maasv1alpha1.MaaSTenant{},
	}
}

// APIPersistObject returns the cluster-persisted MaaSTenant for apply/create paths.
func (w *MaaSTenantPlatform) APIPersistObject() client.Object {
	if w.MaaSTenant == nil {
		return nil
	}
	return w.MaaSTenant
}

func (w *MaaSTenantPlatform) GetStatus() *common.Status {
	w.syncFromTenant()
	return &w.bridge
}

func (w *MaaSTenantPlatform) GetConditions() []common.Condition {
	w.syncFromTenant()
	return w.bridge.Conditions
}

func (w *MaaSTenantPlatform) SetConditions(conditions []common.Condition) {
	w.bridge.Conditions = conditions
	w.syncToTenant()
}

// SyncPlatformStatus copies the common.Status bridge onto MaaSTenant.status (for any future ODH-side writes).
func (w *MaaSTenantPlatform) SyncPlatformStatus() {
	w.syncToTenant()
}

func (w *MaaSTenantPlatform) syncFromTenant() {
	if w.MaaSTenant == nil {
		return
	}
	w.bridge.Phase = w.MaaSTenant.Status.Phase
	w.bridge.Conditions = metav1ConditionsToCommon(w.MaaSTenant.Status.Conditions)
}

func (w *MaaSTenantPlatform) syncToTenant() {
	if w.MaaSTenant == nil {
		return
	}
	w.MaaSTenant.Status.Phase = w.bridge.Phase
	w.MaaSTenant.Status.Conditions = commonConditionsToMetav1(w.bridge.Conditions)
}

// DeepCopyObject implements runtime.Object for the wrapper.
func (w *MaaSTenantPlatform) DeepCopyObject() runtime.Object {
	if w == nil {
		return nil
	}
	var tenantCopy *maasv1alpha1.MaaSTenant
	if w.MaaSTenant != nil {
		tenantCopy = w.MaaSTenant.DeepCopy()
	}
	bridgeCopy := w.bridge.DeepCopy()
	out := &MaaSTenantPlatform{
		MaaSTenant: tenantCopy,
		bridge:     *bridgeCopy,
	}
	return out
}

func metav1ConditionsToCommon(in []metav1.Condition) []common.Condition {
	if len(in) == 0 {
		return nil
	}
	out := make([]common.Condition, 0, len(in))
	for i := range in {
		out = append(out, metav1ConditionToCommon(in[i]))
	}
	return out
}

func metav1ConditionToCommon(c metav1.Condition) common.Condition {
	return common.Condition{
		Type:               c.Type,
		Status:             c.Status,
		Reason:             c.Reason,
		Message:            c.Message,
		ObservedGeneration: c.ObservedGeneration,
		LastTransitionTime: c.LastTransitionTime,
	}
}

func commonConditionsToMetav1(in []common.Condition) []metav1.Condition {
	if len(in) == 0 {
		return nil
	}
	out := make([]metav1.Condition, 0, len(in))
	for i := range in {
		c := in[i]
		out = append(out, metav1.Condition{
			Type:               c.Type,
			Status:             c.Status,
			Reason:             c.Reason,
			Message:            c.Message,
			ObservedGeneration: c.ObservedGeneration,
			LastTransitionTime: c.LastTransitionTime,
		})
	}
	return out
}

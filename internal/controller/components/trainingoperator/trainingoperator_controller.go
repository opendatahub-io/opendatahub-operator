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

package trainingoperator

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

// Deprecated: Training Operator v1 is obsolete in RHOAI 3.6.
// The reconciler is a no-op: it watches the TrainingOperator CR but takes no
// deploy/render/gc actions. Existing cluster resources are left untouched.
// When a customer sets managementState to Removed, the DSC reconciler stops
// creating the CR and Kubernetes cascade GC deletes owned resources.
func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &componentApi.TrainingOperator{}).
		Build(ctx)

	if err != nil {
		return err
	}

	return nil
}

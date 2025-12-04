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

package mlflowoperator

import (
	"context"

	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = append(rr.Manifests, manifestPath())
	return nil
}

// TODO: Add any custom actions specific to MlFlowOperator here if needed.
// For example, if you need to check preconditions before deploying:
//
// func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
//     // Check if required dependencies are installed
//     return nil
// }

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

package modelsasservice

import (
	"context"
	"fmt"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func renderMaasOperatorInstall(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	if _, ok := rr.Instance.(*componentApi.ModelsAsService); !ok {
		return fmt.Errorf("resource instance is not ModelsAsService: %T", rr.Instance)
	}

	out, err := buildMaasOperatorInstallManifests(ctx, rr)
	if err != nil {
		return err
	}
	// Clear resources accumulated earlier in the pipeline so this action only applies the
	// maas-controller install bundle (see deploy.WithApplyOrder for apply ordering).
	rr.Resources = nil
	if err := rr.AddResources(out...); err != nil {
		return fmt.Errorf("add maas-controller install manifest: %w", err)
	}

	// Render and append the deny-by-default gateway policy bundle (AuthPolicy,
	// RateLimitPolicy, etc.) only when the Kuadrant CRDs are present on the
	// cluster. OwnsGVK(...CrdExists...) guards watches but not the deploy path;
	// without this check, apply would fail with "no matches for kind" on
	// clusters without Kuadrant.
	hasPolicyCRDs, err := kuadrantPolicyCRDsExist(ctx, rr)
	if err != nil {
		return fmt.Errorf("check kuadrant policy CRDs: %w", err)
	}

	if hasPolicyCRDs {
		policyOut, err := buildMaasPolicyManifests(rr)
		if err != nil {
			return err
		}
		if len(policyOut) > 0 {
			if err := rr.AddResources(policyOut...); err != nil {
				return fmt.Errorf("add maas-controller policy manifest: %w", err)
			}
		}
	}

	return nil
}

func kuadrantPolicyCRDsExist(ctx context.Context, rr *odhtypes.ReconciliationRequest) (bool, error) {
	has, err := cluster.HasCRD(ctx, rr.Client, gvk.AuthPolicyv1)
	if err != nil {
		return false, err
	}

	return has, nil
}

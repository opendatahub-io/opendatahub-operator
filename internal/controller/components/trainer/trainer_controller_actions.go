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

package trainer

import (
	"context"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

var (
	ErrJobSetOperatorNotInstalled = odherrors.NewStopError(status.JobSetOperatorNotInstalledMessage)
	ErrJobSetOperatorCRNotFound   = odherrors.NewStopError(status.JobSetOperatorCRNotFoundMessage)
)

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	if jobSetInfo, err := cluster.OperatorExists(ctx, rr.Client, jobSetOperator); err != nil || jobSetInfo == nil {
		if err != nil {
			return odherrors.NewStopErrorW(err)
		}

		return ErrJobSetOperatorNotInstalled
	}

	// Check if any JobSetOperator CR exists with a name other than "cluster"
	// This check is done before checking for "cluster" CR to provide better context if user creates wrong named CR
	// This is a workaround for https://issues.redhat.com/browse/OCPBUGS-72507, once JobSetOperator is enforced to have "cluster" as the name, we can remove this check
	jobSetOperatorList := &unstructured.UnstructuredList{}
	jobSetOperatorList.SetGroupVersionKind(gvk.JobSetOperatorV1)
	if err := rr.Client.List(ctx, jobSetOperatorList); err != nil {
		return odherrors.NewStopErrorW(err)
	}
	for _, item := range jobSetOperatorList.Items {
		if item.GetName() != "cluster" {
			return odherrors.NewStopError(status.JobSetOperatorCRWrongNameMessage, item.GetName())
		}
	}

	// Check that JobSetOperator CR exists with name "cluster"
	jobSetOperatorCR := &unstructured.Unstructured{}
	jobSetOperatorCR.SetGroupVersionKind(gvk.JobSetOperatorV1)
	if err := rr.Client.Get(ctx, types.NamespacedName{Name: "cluster"}, jobSetOperatorCR); err != nil {
		if k8serr.IsNotFound(err) {
			return ErrJobSetOperatorCRNotFound
		}
		return odherrors.NewStopErrorW(err)
	}

	jobset, err := cluster.HasCRD(ctx, rr.Client, gvk.JobSetv1alpha2)
	if err != nil {
		return odherrors.NewStopError("failed to check %s CRDs version: %w", gvk.JobSetv1alpha2, err)
	}

	if !jobset {
		return odherrors.NewStopError(status.JobSetCRDMissingMessage)
	}

	return nil
}

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = append(rr.Manifests, manifestPath())
	return nil
}

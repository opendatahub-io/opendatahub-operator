package monitor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"
	apihelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// RequiredAPI identifies an API that must be usable before an action runs.
type RequiredAPI struct {
	CRDName string

	// GVK.Version enables the served-version check.
	GVK schema.GroupVersionKind
}

// CheckRequiredAPIs verifies CRD presence, lifecycle, and an optional served version.
// A CRD that is not yet Established is Unknown because it may still be installing.
// Definite failures take precedence over Unknown results, while preserving all findings.
//
// The CRDs are read by name rather than through the RESTMapper, to avoid a race
// where the discovery cache lags behind the EstablishingController and reports an
// Established CRD as absent. Read errors are returned to the caller unless another
// required CRD has already been confirmed unusable; in that case the definite
// failure wins and the read error is included in the result message.
func CheckRequiredAPIs(ctx context.Context, cli client.Client, apis []RequiredAPI) (CheckResult, error) {
	if len(apis) == 0 {
		return CheckResult{}, errors.New("CheckRequiredAPIs: called with an empty required API list")
	}

	var unusable []string
	var indeterminate []string
	var readErrors *multierror.Error

	for _, api := range apis {
		if api.CRDName == "" {
			return CheckResult{}, errors.New("CheckRequiredAPIs: RequiredAPI.CRDName must not be empty")
		}

		result, err := checkRequiredAPI(ctx, cli, api)
		if err != nil {
			readErrors = multierror.Append(readErrors, err)
			continue
		}

		switch result.ConditionStatus() {
		case metav1.ConditionFalse:
			unusable = append(unusable, result.Message)
		case metav1.ConditionUnknown:
			indeterminate = append(indeterminate, result.Message)
		case metav1.ConditionTrue:
		}
	}

	if len(unusable) > 0 {
		if readErrors != nil {
			for _, readErr := range readErrors.Errors {
				indeterminate = append(indeterminate, readErr.Error())
			}
		}

		return Failed("%s", strings.Join(append(unusable, indeterminate...), "; ")), nil
	}
	if readErrors != nil {
		return CheckResult{}, readErrors.ErrorOrNil()
	}

	if len(indeterminate) > 0 {
		return Indeterminate("%s", strings.Join(indeterminate, "; ")), nil
	}

	return Passed(), nil
}

func checkRequiredAPI(ctx context.Context, cli client.Client, api RequiredAPI) (CheckResult, error) {
	crd, err := cluster.GetCRD(ctx, cli, api.CRDName)
	switch {
	case k8serr.IsNotFound(err):
		return Failed("%s: CRD not found", api.CRDName), nil
	case err != nil:
		return CheckResult{}, fmt.Errorf("%s: failed to check CRD presence: %w", api.CRDName, err)
	}

	if apihelpers.IsCRDConditionTrue(&crd, apiextensionsv1.Terminating) {
		return Failed("%s: CRD is terminating", api.CRDName), nil
	}

	if version := api.GVK.Version; version != "" && !servesVersion(&crd, version) {
		return Failed("%s: CRD does not serve version %s", api.CRDName, version), nil
	}

	if !apihelpers.IsCRDConditionTrue(&crd, apiextensionsv1.Established) {
		return Indeterminate("%s: CRD is not established", api.CRDName), nil
	}

	return Passed(), nil
}

func servesVersion(crd *apiextensionsv1.CustomResourceDefinition, version string) bool {
	for i := range crd.Spec.Versions {
		if crd.Spec.Versions[i].Name == version {
			return crd.Spec.Versions[i].Served
		}
	}

	return false
}

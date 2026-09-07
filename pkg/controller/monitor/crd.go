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

// RequiredAPI declares an API resource that the operator will create or read.
//
// A required API is usable only when its CRD exists, is Established, is not
// Terminating, and serves the version the operator calls. See [CheckRequiredAPIs].
type RequiredAPI struct {
	// CRDName is the metadata.name of the CustomResourceDefinition that serves
	// the API, in "<plural>.<group>" form. Required.
	CRDName string

	// GVK is the group/version/kind the operator uses. Optional.
	//
	// When GVK.Version is set, the CRD must list that version in spec.versions
	// with served: true. When it is empty, the served-version check is skipped
	// and only existence, Established and Terminating are checked.
	GVK schema.GroupVersionKind
}

// CheckRequiredAPIs verifies that every declared API is usable.
//
// For each API, the CRD must:
//
//   - exist;
//   - not have Terminating=True in status.conditions;
//   - have Established=True in status.conditions; and
//   - serve the declared version, when one is declared: the entry in
//     spec.versions whose name matches must have served: true.
//
// Established and Terminating are CRD status conditions; served is a property of
// an entry in spec.versions, not a status condition.
//
// A missing, terminating, or non-serving CRD is direct evidence of absence and
// yields a False result. A CRD that is not yet Established is genuinely
// indeterminate — it can be transient (still installing) or permanent (the CRD was
// rejected) — and yields an Unknown result so the caller requeues rather than
// declaring the dependency broken. When both occur, the result is False and the
// message reports every finding.
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
			// usable, nothing to report
		}
	}

	if len(unusable) > 0 {
		if readErrors != nil {
			for _, readErr := range readErrors.Errors {
				indeterminate = append(indeterminate, readErr.Error())
			}
		}

		// Report the indeterminate findings too, but the outcome is a definite failure.
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

// servesVersion reports whether the CRD lists version with served: true.
func servesVersion(crd *apiextensionsv1.CustomResourceDefinition, version string) bool {
	for i := range crd.Spec.Versions {
		if crd.Spec.Versions[i].Name == version {
			return crd.Spec.Versions[i].Served
		}
	}

	return false
}

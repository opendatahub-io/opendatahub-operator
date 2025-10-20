// +kubebuilder:skip
package dashboard

import (
	"context"
	"errors"
	"fmt"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = []odhtypes.ManifestInfo{defaultManifestInfo(rr.Release.Name)}

	return nil
}

func customizeResources(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	for i := range rr.Resources {
		if rr.Resources[i].GroupVersionKind() == gvk.OdhDashboardConfig {
			// mark the resource as not supposed to be managed by the operator
			resources.SetAnnotation(&rr.Resources[i], annotations.ManagedByODHOperator, "false")
			break
		}
	}

	return nil
}

func setKustomizedParams(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	extraParamsMap, err := computeKustomizeVariable(ctx, rr.Client, rr.Release.Name)
	if err != nil {
		return errors.New("failed to set variable for url, section-title etc")
	}

	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), "params.env", nil, extraParamsMap); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", rr.Manifests[0].String(), err)
	}
	return nil
}

func configureDependencies(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	if rr.Release.Name == cluster.OpenDataHub {
		return nil
	}

	err := rr.AddResources(&corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "anaconda-ce-access",
			Namespace: rr.DSCI.Spec.ApplicationsNamespace,
		},
		Type: corev1.SecretTypeOpaque,
	})

	if err != nil {
		return fmt.Errorf("failed to create access-secret for anaconda: %w", err)
	}

	return nil
}

func updateStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	d, ok := rr.Instance.(*componentApi.Dashboard)
	if !ok {
		return errors.New("instance is not of type *odhTypes.Dashboard")
	}

	// url
	rl := routev1.RouteList{}
	err := rr.Client.List(
		ctx,
		&rl,
		client.InNamespace(rr.DSCI.Spec.ApplicationsNamespace),
		client.MatchingLabels(map[string]string{
			labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
		}),
	)

	if err != nil {
		return fmt.Errorf("failed to list routes: %w", err)
	}

	d.Status.URL = ""
	if len(rl.Items) == 1 {
		d.Status.URL = resources.IngressHost(rl.Items[0])
	}

	return nil
}

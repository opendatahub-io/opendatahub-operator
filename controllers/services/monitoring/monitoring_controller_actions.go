package monitoring

import (
	"context"
	"errors"
	"fmt"

	routev1 "github.com/openshift/api/route/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	return nil
}

func updateStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	d, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	// url
	rl := routev1.RouteList{}
	err := rr.Client.List(
		ctx,
		&rl,
		client.InNamespace(rr.DSCI.Spec.Monitoring.Namespace),
		client.MatchingLabels(map[string]string{
			labels.PlatformPartOf: serviceApi.MonitoringServiceName,
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

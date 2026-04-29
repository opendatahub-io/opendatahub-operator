package monitoring

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
)

const (
	ServiceName = serviceApi.MonitoringServiceName
)

func isComponentReady(ctx context.Context, cli client.Client, obj common.PlatformObject) (bool, error) {
	err := cli.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	switch {
	case k8serr.IsNotFound(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("failed to get component instance: %w", err)
	default:
		return conditions.IsStatusConditionTrue(obj.GetStatus(), status.ConditionTypeReady), nil
	}
}

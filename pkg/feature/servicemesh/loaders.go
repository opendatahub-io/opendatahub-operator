package servicemesh

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func ClusterDetails(ctx context.Context, f *feature.Feature) error {
	data := f.Spec

	if domain, err := cluster.GetDomain(ctx, f.Client); err == nil {
		data.Domain = domain
	} else {
		return errors.WithStack(err)
	}

	return nil
}

func ResolveAuthNamespace(f *feature.Feature) error {
	dsciAuthNamespace := strings.TrimSpace(f.Spec.Auth.Namespace)

	if len(dsciAuthNamespace) == 0 {
		f.Spec.Auth.Namespace = f.Spec.AppNamespace + "-auth-provider"
	}

	return nil
}

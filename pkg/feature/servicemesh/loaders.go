package servicemesh

import (
	"strings"

	"github.com/pkg/errors"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func ClusterDetails(f *feature.Feature) error {
	data := f.Spec

	if domain, err := cluster.GetDomain(f.DynamicClient); err == nil {
		data.Domain = domain
	} else {
		return errors.WithStack(err)
	}

	return nil
}

func ResolveAuthNamespace(f *feature.Feature) error {
	dsciAuthNamespace := strings.TrimSpace(f.Spec.AuthorinoConfigs.Namespace)

	if len(dsciAuthNamespace) == 0 {
		f.Spec.AuthorinoConfigs.Namespace = strings.TrimSuffix(f.Spec.AppNamespace, "-applications") + "-auth-provider"
	}

	return nil
}

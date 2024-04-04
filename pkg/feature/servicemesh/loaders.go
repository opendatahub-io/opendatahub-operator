package servicemesh

import (
	"strings"

	"github.com/pkg/errors"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func ClusterDetails(f *feature.Feature) error {
	data := f.Spec

	if domain, err := cluster.GetDomain(f.Client); err == nil {
		data.Domain = domain
	} else {
		return errors.WithStack(err)
	}

	return nil
}

func CreateAuthNamespace(authNs, appNs string) string {
	dsciAuthNamespace := strings.TrimSpace(authNs)

	if len(dsciAuthNamespace) == 0 {
		return appNs + "-auth-provider"
	}

	return dsciAuthNamespace
}

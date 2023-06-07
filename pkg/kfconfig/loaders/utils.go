package loaders

import (
	kftypesv3 "github.com/opendatahub-io/opendatahub-operator/apis/apps"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig"
)

func maybeGetPlatform(pluginKind string) string {
	platforms := map[string]string{
		string(kfconfig.AWS_PLUGIN_KIND):              kftypesv3.AWS,
		string(kfconfig.GCP_PLUGIN_KIND):              kftypesv3.GCP,
		string(kfconfig.EXISTING_ARRIKTO_PLUGIN_KIND): kftypesv3.EXISTING_ARRIKTO,
		string(kfconfig.OSSM_PLUGIN_KIND):             kftypesv3.OSSM,
	}

	p, ok := platforms[pluginKind]
	if ok {
		return p
	} else {
		return ""
	}
}

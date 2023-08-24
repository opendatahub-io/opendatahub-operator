package feature

import (
	"github.com/opendatahub-io/opendatahub-operator/apis/ossm.plugins.kubeflow.org/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig/ossmplugin"
	"strings"
)

type Spec struct {
	*ossmplugin.OssmPluginSpec
	OAuth   OAuth
	Domain  string
	Tracker *v1alpha1.OssmResourceTracker
}

type OAuth struct {
	AuthzEndpoint,
	TokenEndpoint,
	Route,
	Port,
	ClientSecret,
	Hmac string
}

func ReplaceChar(s string, oldChar, newChar string) string {
	return strings.ReplaceAll(s, oldChar, newChar)
}

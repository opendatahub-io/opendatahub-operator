package feature

import (
	"strings"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
)

type Spec struct {
	*infrav1.ServiceMeshSpec
	Serving                  *infrav1.ServingSpec
	OAuth                    OAuth
	AppNamespace             string
	Domain                   string
	KnativeCertificateSecret string
	KnativeIngressDomain     string
	Source                   *featurev1.Source
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

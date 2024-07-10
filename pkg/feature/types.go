package feature

import (
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
)

type Spec struct {
	*infrav1.ServiceMeshSpec
	Serving                  *infrav1.ServingSpec
	AuthProviderName         string
	OAuth                    OAuth
	AppNamespace             string
	TargetNamespace          string
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

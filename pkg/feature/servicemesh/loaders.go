package servicemesh

import (
	"github.com/pkg/errors"

	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/secretgenerator"
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

func OAuthConfig(f *feature.Feature) error {
	data := f.Spec

	var err error
	var clientSecret, hmac *secretgenerator.Secret
	if clientSecret, err = secretgenerator.NewSecret("ossm-odh-oauth", "random", 32); err != nil {
		return errors.WithStack(err)
	}

	if hmac, err = secretgenerator.NewSecret("ossm-odh-hmac", "random", 32); err != nil {
		return errors.WithStack(err)
	}

	if oauthServerDetailsJSON, err := cluster.GetOAuthServerDetails(); err == nil {
		hostName, port, errURLParsing := cluster.ExtractHostNameAndPort(oauthServerDetailsJSON.Get("issuer").MustString("issuer"))
		if errURLParsing != nil {
			return errURLParsing
		}

		data.OAuth = feature.OAuth{
			AuthzEndpoint: oauthServerDetailsJSON.Get("authorization_endpoint").MustString("authorization_endpoint"),
			TokenEndpoint: oauthServerDetailsJSON.Get("token_endpoint").MustString("token_endpoint"),
			Route:         hostName,
			Port:          port,
			ClientSecret:  clientSecret.Value,
			Hmac:          hmac.Value,
		}
	} else {
		return errors.WithStack(err)
	}

	return nil
}

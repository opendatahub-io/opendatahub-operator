package feature

import (
	"github.com/opendatahub-io/opendatahub-operator/pkg/secret"
	"github.com/pkg/errors"
)

func ClusterDetails(feature *Feature) error {
	data := feature.Spec

	if domain, err := GetDomain(feature.dynamicClient); err == nil {
		data.Domain = domain
	} else {
		return errors.WithStack(err)
	}

	return nil
}

func OAuthConfig(feature *Feature) error {
	data := feature.Spec

	var err error
	var clientSecret, hmac *secret.Secret
	if clientSecret, err = secret.NewSecret("ossm-odh-oauth", "random", 32); err != nil {
		return errors.WithStack(err)
	}

	if hmac, err = secret.NewSecret("ossm-odh-hmac", "random", 32); err != nil {
		return errors.WithStack(err)
	}

	if oauthServerDetailsJson, err := GetOAuthServerDetails(); err == nil {
		hostName, port, errUrlParsing := ExtractHostNameAndPort(oauthServerDetailsJson.Get("issuer").MustString("issuer"))
		if errUrlParsing != nil {
			return errUrlParsing
		}

		data.OAuth = OAuth{
			AuthzEndpoint: oauthServerDetailsJson.Get("authorization_endpoint").MustString("authorization_endpoint"),
			TokenEndpoint: oauthServerDetailsJson.Get("token_endpoint").MustString("token_endpoint"),
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

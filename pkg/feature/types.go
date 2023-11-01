package feature

import (
	v1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"strings"
)

type Spec struct {
	OAuth        OAuth
	AppNamespace string
	Domain       string
	Tracker      *v1.FeatureTracker
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

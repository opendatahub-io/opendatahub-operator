package feature

import (
	"strings"

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
	AlertmanagerData         *AlertmanagerData
	MonNamespace             string
}

type OAuth struct {
	AuthzEndpoint,
	TokenEndpoint,
	Route,
	Port,
	ClientSecret,
	Hmac string
}

type AlertmanagerData struct {
	SMTPHost string // smtp_host
	SMTPPort string // smtp_port
	SMTPUSER string // smtp_username
	// SMTPPSD           string // smtp_password
	SMTPDoamin        string //  value devshift.net or rhmw.io
	PagerDutyKey      string // pagerduty_token
	PagerDutyReceiver string // value PagerDuty or alerts-sink
	NotificationEmail string // notification-email
	DeadManSnitchURL  string // snitch_url
	EmailSubject      string // email subject
	EmailBody         string // email body
}

func ReplaceChar(s string, oldChar, newChar string) string {
	return strings.ReplaceAll(s, oldChar, newChar)
}

package servicemesh

import (
	"bytes"
	"fmt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"text/template"
)

const tokenSecret = `
resources:
- "@type": "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.Secret"
  name: token
  generic_secret:
    secret:
      inline_string: "{{ .Secret }}"
`

const hmacSecret = `
resources:
- "@type": "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.Secret"
  name: hmac
  generic_secret:
    secret:
      inline_bytes: "{{ .Secret }}"
`

func createEnvoySecret(oAuth feature.OAuth, objectMeta metav1.ObjectMeta) (*corev1.Secret, error) {
	clientSecret, err := processInlineTemplate(tokenSecret, struct{ Secret string }{Secret: oAuth.ClientSecret})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	hmacSecret, err := processInlineTemplate(hmacSecret, struct{ Secret string }{Secret: oAuth.Hmac})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &corev1.Secret{
		ObjectMeta: objectMeta,
		Data: map[string][]byte{
			"token-secret.yaml": clientSecret,
			"hmac-secret.yaml":  hmacSecret,
		},
	}, nil
}

func processInlineTemplate(templateString string, data interface{}) ([]byte, error) {
	tmpl, err := template.New("inline-template").Parse(templateString)
	if err != nil {
		return nil, fmt.Errorf("error parsing template: %v", err)
	}

	var output bytes.Buffer
	err = tmpl.Execute(&output, data)
	if err != nil {
		return nil, fmt.Errorf("error executing template: %v", err)
	}

	return output.Bytes(), nil
}

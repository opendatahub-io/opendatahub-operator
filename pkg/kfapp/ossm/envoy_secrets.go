package ossm

import (
	"bytes"
	"context"
	"fmt"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

func (o *OssmInstaller) createEnvoySecret(oAuth oAuth, objectMeta metav1.ObjectMeta) error {

	clientSecret, err := processInlineTemplate(tokenSecret, struct{ Secret string }{Secret: oAuth.ClientSecret})
	if err != nil {
		return errors.WithStack(err)
	}

	hmacSecret, err := processInlineTemplate(hmacSecret, struct{ Secret string }{Secret: oAuth.Hmac})
	if err != nil {
		return errors.WithStack(err)
	}

	objectMeta.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: o.tracker.APIVersion,
			Kind:       o.tracker.Kind,
			Name:       o.tracker.Name,
			UID:        o.tracker.UID,
		},
	})

	secret := &corev1.Secret{
		ObjectMeta: objectMeta,
		Data: map[string][]byte{
			"token-secret.yaml": clientSecret,
			"hmac-secret.yaml":  hmacSecret,
		},
	}

	clientset, err := kubernetes.NewForConfig(o.config)
	if err != nil {
		return errors.WithStack(err)
	}

	_, err = clientset.CoreV1().
		Secrets(objectMeta.Namespace).
		Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return errors.WithStack(err)
	}

	return nil
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

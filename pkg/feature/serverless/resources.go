package serverless

import (
	"fmt"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func ServingCertificateResource(f *feature.Feature) error {
	domain := strings.TrimSpace(f.Spec.Serving.IngressGateway.Domain)
	if len(domain) == 0 {
		var errDomain error
		domain, errDomain = GetDomain(f.DynamicClient)
		if errDomain != nil {
			return fmt.Errorf("failed to fetch OpenShift domain to generate certificate for Serverless: %w", errDomain)
		}

		domain = "*." + domain
	}
	return f.CreateSelfSignedCertificate(f.Spec.Serving.IngressGateway.Certificate, domain, f.Spec.Mesh.Namespace)
}

/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gateway

import (
	"context"
	"errors"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func createGatewayInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createGatewayInfrastructure")

	gatewayInstance, ok := rr.Instance.(*serviceApi.Gateway)
	if !ok {
		return errors.New("failed to cast the reconciliation request instance to Gateway")
	}
	l.Info("Creating Gateway infrastructure", "gateway", gatewayInstance.Name)

	if err := createGatewayClass(rr); err != nil {
		return fmt.Errorf("failed to create GatewayClass: %w", err)
	}

	certSecretName, err := handleCertificates(ctx, rr, gatewayInstance)
	if err != nil {
		return fmt.Errorf("failed to handle certificates: %w", err)
	}

	if err := createGateway(rr, gatewayInstance, certSecretName); err != nil {
		return fmt.Errorf("failed to create Gateway: %w", err)
	}

	l.Info("Successfully created Gateway infrastructure",
		"gateway", "odh-gateway",
		"namespace", gatewayInstance.Spec.Namespace,
		"domain", gatewayInstance.Spec.Domain,
		"certificateType", gatewayInstance.Spec.Certificates.Type)

	return nil
}

func createGatewayClass(rr *odhtypes.ReconciliationRequest) error {
	gatewayClass := &gwapiv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "odh-gateway-class",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "opendatahub-operator",
				"opendatahub.io/internal":      "true",
			},
		},
		Spec: gwapiv1.GatewayClassSpec{
			ControllerName: "openshift.io/gateway-controller/v1",
		},
	}

	return rr.AddResources(gatewayClass)
}

func handleCertificates(ctx context.Context, rr *odhtypes.ReconciliationRequest, gatewayInstance *serviceApi.Gateway) (string, error) {
	l := logf.FromContext(ctx).WithName("handleCertificates")

	certType := gatewayInstance.Spec.Certificates.Type

	switch certType {
	case "cert-manager":
		return createCertManagerCertificate(rr, gatewayInstance)
	case "user-provided":
		l.Info("User-provided certificates not yet implemented")
		return "", nil
	case "openshift-service-ca":
		l.Info("OpenShift service CA not yet implemented")
		return "", nil
	default:
		l.Info("No certificate type specified, HTTP only", "type", certType)
		return "", nil
	}
}

func createCertManagerCertificate(rr *odhtypes.ReconciliationRequest, gatewayInstance *serviceApi.Gateway) (string, error) {
	secretName := fmt.Sprintf("%s-tls", gatewayInstance.Name)

	issuerRef := cmmeta.ObjectReference{
		Name: "selfsigned-cluster-issuer",
		Kind: "ClusterIssuer",
	}

	if gatewayInstance.Spec.Certificates.IssuerRef != nil {
		issuerRef = *gatewayInstance.Spec.Certificates.IssuerRef
	}

	certificate := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-cert", gatewayInstance.Name),
			Namespace: gatewayInstance.Spec.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "opendatahub-operator",
				"opendatahub.io/internal":      "true",
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: secretName,
			DNSNames:   []string{gatewayInstance.Spec.Domain},
			IssuerRef:  issuerRef,
		},
	}

	if err := rr.AddResources(certificate); err != nil {
		return "", fmt.Errorf("failed to add Certificate resource: %w", err)
	}

	return secretName, nil
}

func createGateway(rr *odhtypes.ReconciliationRequest, gatewayInstance *serviceApi.Gateway, certSecretName string) error {
	listeners := createListeners(certSecretName)

	gateway := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odh-gateway",
			Namespace: gatewayInstance.Spec.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "opendatahub-operator",
				"opendatahub.io/internal":      "true",
			},
		},
		Spec: gwapiv1.GatewaySpec{
			GatewayClassName: "odh-gateway-class",
			Listeners:        listeners,
		},
	}

	return rr.AddResources(gateway)
}

func createListeners(certSecretName string) []gwapiv1.Listener {
	listeners := []gwapiv1.Listener{}

	if certSecretName != "" {
		httpsMode := gwapiv1.TLSModeTerminate
		httpsListener := gwapiv1.Listener{
			Name:     "https",
			Protocol: gwapiv1.HTTPSProtocolType,
			Port:     443,
			TLS: &gwapiv1.GatewayTLSConfig{
				Mode: &httpsMode,
				CertificateRefs: []gwapiv1.SecretObjectReference{
					{
						Name: gwapiv1.ObjectName(certSecretName),
					},
				},
			},
		}
		listeners = append(listeners, httpsListener)
	}

	return listeners
}

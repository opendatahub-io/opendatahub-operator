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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func createGatewayInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createGatewayInfrastructure")

	gatewayInstance, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("failed to cast the reconciliation request instance to GatewayConfig")
	}
	l.Info("Creating Gateway infrastructure", "gateway", gatewayInstance.Name)

	domain := gatewayInstance.Spec.Domain
	if domain == "" {
		clusterDomain, err := cluster.GetDomain(ctx, rr.Client)
		if err != nil {
			return fmt.Errorf("failed to get cluster domain: %w", err)
		}
		domain = "odh-gateway." + clusterDomain
	}

	if err := createGatewayClass(rr); err != nil {
		return fmt.Errorf("failed to create GatewayClass: %w", err)
	}

	certSecretName, err := handleCertificates(ctx, rr, gatewayInstance, domain)
	if err != nil {
		return fmt.Errorf("failed to handle certificates: %w", err)
	}

	if err := createGateway(rr, gatewayInstance, certSecretName, domain); err != nil {
		return fmt.Errorf("failed to create Gateway: %w", err)
	}

	l.Info("Successfully created Gateway infrastructure",
		"gateway", "odh-gateway",
		"namespace", gatewayInstance.Spec.Namespace,
		"domain", domain,
		"certificateType", getCertificateType(gatewayInstance))

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

func handleCertificates(ctx context.Context, rr *odhtypes.ReconciliationRequest, gatewayInstance *serviceApi.GatewayConfig, domain string) (string, error) {
	certConfig := gatewayInstance.Spec.Certificate
	if certConfig == nil {
		certConfig = &infrav1.CertificateSpec{
			Type: infrav1.OpenshiftDefaultIngress,
		}
	}

	secretName := certConfig.SecretName
	if secretName == "" {
		secretName = fmt.Sprintf("%s-tls", gatewayInstance.Name)
	}

	switch certConfig.Type {
	case infrav1.OpenshiftDefaultIngress:
		return handleOpenshiftDefaultCertificate(ctx, rr, gatewayInstance, secretName)
	case infrav1.SelfSigned:
		return handleSelfSignedCertificate(ctx, rr, gatewayInstance, secretName, domain)
	case infrav1.Provided:
		return secretName, nil
	default:
		return "", fmt.Errorf("unsupported certificate type: %s", certConfig.Type)
	}
}

func handleOpenshiftDefaultCertificate(ctx context.Context, rr *odhtypes.ReconciliationRequest, gatewayInstance *serviceApi.GatewayConfig, secretName string) (string, error) {
	err := cluster.PropagateDefaultIngressCertificate(ctx, rr.Client, secretName, gatewayInstance.Spec.Namespace)
	if err != nil {
		return "", fmt.Errorf("failed to propagate default ingress certificate: %w", err)
	}

	return secretName, nil
}

func handleSelfSignedCertificate(ctx context.Context, rr *odhtypes.ReconciliationRequest,
	gatewayInstance *serviceApi.GatewayConfig, secretName string, domain string) (string, error) {
	hostname := "odh-gateway." + domain

	err := cluster.CreateSelfSignedCertificate(
		ctx,
		rr.Client,
		secretName,
		hostname,
		gatewayInstance.Spec.Namespace,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create self-signed certificate: %w", err)
	}

	return secretName, nil
}

func getCertificateType(gatewayInstance *serviceApi.GatewayConfig) string {
	if gatewayInstance.Spec.Certificate == nil {
		return string(infrav1.OpenshiftDefaultIngress)
	}
	return string(gatewayInstance.Spec.Certificate.Type)
}

func createGateway(rr *odhtypes.ReconciliationRequest, gatewayInstance *serviceApi.GatewayConfig, certSecretName string, domain string) error {
	listeners := createListeners(certSecretName, domain)

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

func createListeners(certSecretName string, domain string) []gwapiv1.Listener {
	listeners := []gwapiv1.Listener{}

	if certSecretName != "" {
		from := gwapiv1.NamespacesFromAll
		httpsMode := gwapiv1.TLSModeTerminate
		hostname := gwapiv1.Hostname(domain)
		httpsListener := gwapiv1.Listener{
			Name:     "https",
			Protocol: gwapiv1.HTTPSProtocolType,
			Port:     443,
			Hostname: &hostname,
			TLS: &gwapiv1.GatewayTLSConfig{
				Mode: &httpsMode,
				CertificateRefs: []gwapiv1.SecretObjectReference{
					{
						Name: gwapiv1.ObjectName(certSecretName),
					},
				},
			},
			AllowedRoutes: &gwapiv1.AllowedRoutes{
				Namespaces: &gwapiv1.RouteNamespaces{
					From: &from,
				},
			},
		}
		listeners = append(listeners, httpsListener)
	}

	return listeners
}

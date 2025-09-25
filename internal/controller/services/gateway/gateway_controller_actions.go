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
	"embed"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

//go:embed resources/*.yaml
var gatewayResources embed.FS

const (
	gatewayNamespace = "openshift-ingress"
	gatewayName      = "odh-gateway"
	gatewayClassName = "odh-gateway-class"
)

func createGatewayInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createGatewayInfrastructure")

	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}
	l.V(1).Info("Creating Gateway infrastructure", "gateway", gatewayConfig.Name)

	domain := gatewayConfig.Spec.Domain
	if domain == "" {
		clusterDomain, err := cluster.GetDomain(ctx, rr.Client)
		if err != nil {
			return fmt.Errorf("failed to get cluster domain: %w", err)
		}
		domain = fmt.Sprintf("%s.%s", gatewayName, clusterDomain)
	}

	if err := createGatewayClass(rr); err != nil {
		return fmt.Errorf("failed to create GatewayClass: %w", err)
	}

	certSecretName, err := handleCertificates(ctx, rr, gatewayConfig, domain)
	if err != nil {
		return fmt.Errorf("failed to handle certificates: %w", err)
	}

	if err := createGateway(rr, certSecretName, domain); err != nil {
		return fmt.Errorf("failed to create Gateway: %w", err)
	}

	l.V(1).Info("Successfully created Gateway infrastructure",
		"gateway", gatewayName,
		"namespace", gatewayNamespace,
		"domain", domain,
		"certificateType", getCertificateType(gatewayConfig))

	return nil
}

func createGatewayClass(rr *odhtypes.ReconciliationRequest) error {
	gatewayClass := &gwapiv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassName,
		},
		Spec: gwapiv1.GatewayClassSpec{
			ControllerName: "openshift.io/gateway-controller/v1",
		},
	}

	return rr.AddResources(gatewayClass)
}

func handleCertificates(ctx context.Context, rr *odhtypes.ReconciliationRequest, gatewayConfig *serviceApi.GatewayConfig, domain string) (string, error) {
	certConfig := gatewayConfig.Spec.Certificate
	if certConfig == nil {
		certConfig = &infrav1.CertificateSpec{
			Type: infrav1.OpenshiftDefaultIngress,
		}
	}

	secretName := certConfig.SecretName
	if secretName == "" {
		secretName = fmt.Sprintf("%s-tls", gatewayConfig.Name)
	}

	switch certConfig.Type {
	case infrav1.OpenshiftDefaultIngress:
		return handleOpenshiftDefaultCertificate(ctx, rr, secretName)
	case infrav1.SelfSigned:
		return handleSelfSignedCertificate(ctx, rr, secretName, domain)
	case infrav1.Provided:
		return secretName, nil
	default:
		return "", fmt.Errorf("unsupported certificate type: %s", certConfig.Type)
	}
}

func handleOpenshiftDefaultCertificate(ctx context.Context, rr *odhtypes.ReconciliationRequest, secretName string) (string, error) {
	err := cluster.PropagateDefaultIngressCertificate(ctx, rr.Client, secretName, gatewayNamespace)
	if err != nil {
		return "", fmt.Errorf("failed to propagate default ingress certificate: %w", err)
	}

	return secretName, nil
}

func handleSelfSignedCertificate(ctx context.Context, rr *odhtypes.ReconciliationRequest, secretName string, domain string) (string, error) {
	hostname := fmt.Sprintf("%s.%s", gatewayName, domain)

	err := cluster.CreateSelfSignedCertificate(
		ctx,
		rr.Client,
		secretName,
		hostname,
		gatewayNamespace,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create self-signed certificate: %w", err)
	}

	return secretName, nil
}

func getCertificateType(gatewayConfig *serviceApi.GatewayConfig) string {
	if gatewayConfig.Spec.Certificate == nil {
		return string(infrav1.OpenshiftDefaultIngress)
	}
	return string(gatewayConfig.Spec.Certificate.Type)
}

func createGateway(rr *odhtypes.ReconciliationRequest, certSecretName string, domain string) error {
	listeners := createListeners(certSecretName, domain)

	gateway := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayName,
			Namespace: gatewayNamespace,
		},
		Spec: gwapiv1.GatewaySpec{
			GatewayClassName: gatewayClassName,
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

func createDestinationRule(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createDestinationRule")
	l.V(1).Info("Creating DestinationRule for TLS configuration")

	// using yaml templates due to complexity of k8s api struct for destination rule
	yamlContent, err := gatewayResources.ReadFile("resources/destinationrule-tls.yaml")
	if err != nil {
		return fmt.Errorf("failed to read DestinationRule template: %w", err)
	}

	decoder := serializer.NewCodecFactory(rr.Client.Scheme()).UniversalDeserializer()
	unstructuredObjects, err := resources.Decode(decoder, yamlContent)
	if err != nil {
		return fmt.Errorf("failed to decode DestinationRule YAML: %w", err)
	}

	if len(unstructuredObjects) != 1 {
		return fmt.Errorf("expected exactly 1 DestinationRule object, got %d", len(unstructuredObjects))
	}

	l.V(1).Info("Successfully created DestinationRule configuration")
	return rr.AddResources(&unstructuredObjects[0])
}

func syncGatewayConfigStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	gateway := &gwapiv1.Gateway{}
	err := rr.Client.Get(ctx, types.NamespacedName{
		Name:      gatewayName,
		Namespace: gatewayNamespace,
	}, gateway)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			gatewayConfig.SetConditions([]common.Condition{
				{
					Type:    status.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  status.NotReadyReason,
					Message: status.GatewayNotFoundMessage,
				},
			})
			return nil
		}
		return fmt.Errorf("failed to get Gateway: %w", err)
	}

	isReady := false
	for _, condition := range gateway.Status.Conditions {
		if condition.Type == string(gwapiv1.GatewayConditionAccepted) && condition.Status == metav1.ConditionTrue {
			isReady = true
			break
		}
	}

	conditionStatus := metav1.ConditionFalse
	reason := status.NotReadyReason
	message := status.GatewayNotReadyMessage

	if isReady {
		conditionStatus = metav1.ConditionTrue
		reason = status.ReadyReason
		message = status.GatewayReadyMessage
	}

	gatewayConfig.SetConditions([]common.Condition{
		{
			Type:    status.ConditionTypeReady,
			Status:  conditionStatus,
			Reason:  reason,
			Message: message,
		},
	})

	return nil
}

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
	"bytes"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

//go:embed resources
var gatewayResources embed.FS

// This helper reduces code duplication and improves error handling consistency.
func validateGatewayConfig(rr *odhtypes.ReconciliationRequest) (*serviceApi.GatewayConfig, error) {
	if rr == nil {
		return nil, errors.New("reconciliation request cannot be nil")
	}
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return nil, errors.New("instance is not of type *services.GatewayConfig")
	}
	if gatewayConfig == nil {
		return nil, errors.New("gatewayConfig cannot be nil")
	}
	return gatewayConfig, nil
}

func createGatewayInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createGatewayInfrastructure")

	// Use helper function for consistent validation
	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}
	l.V(1).Info("Creating Gateway infrastructure", "gateway", gatewayConfig.Name)

	domain, err := ResolveDomain(ctx, rr.Client, gatewayConfig)
	if err != nil {
		return fmt.Errorf("failed to resolve domain: %w", err)
	}

	if err := createGatewayClass(rr); err != nil {
		return fmt.Errorf("failed to create GatewayClass: %w", err)
	}

	certSecretName, err := handleCertificates(ctx, rr, gatewayConfig, domain)
	if err != nil {
		return fmt.Errorf("failed to handle certificates: %w", err)
	}

	if err := createGateway(rr, certSecretName, domain, DefaultGatewayName); err != nil {
		return fmt.Errorf("failed to create Gateway: %w", err)
	}

	l.V(1).Info("Successfully created Gateway infrastructure",
		"gateway", DefaultGatewayName,
		"namespace", GatewayNamespace,
		"domain", domain,
		"certificateType", GetCertificateType(gatewayConfig))

	return nil
}

func createGatewayClass(rr *odhtypes.ReconciliationRequest) error {
	gatewayClass := &gwapiv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: GatewayClassName,
		},
		Spec: gwapiv1.GatewayClassSpec{
			ControllerName: gwapiv1.GatewayController(GatewayControllerName),
		},
	}

	return rr.AddResources(gatewayClass)
}

func handleCertificates(ctx context.Context, rr *odhtypes.ReconciliationRequest, gatewayConfig *serviceApi.GatewayConfig, domain string) (string, error) {
	// Input validation
	if gatewayConfig == nil {
		return "", errors.New("gatewayConfig cannot be nil")
	}
	if domain == "" {
		return "", errors.New("domain cannot be empty")
	}

	// Get certificate configuration with default fallback
	certConfig := gatewayConfig.Spec.Certificate
	if certConfig == nil {
		certConfig = &infrav1.CertificateSpec{
			Type: infrav1.OpenshiftDefaultIngress,
		}
	}

	// Generate secret name with fallback
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
	err := cluster.PropagateDefaultIngressCertificate(ctx, rr.Client, secretName, GatewayNamespace)
	if err != nil {
		return "", fmt.Errorf("failed to propagate default ingress certificate: %w", err)
	}

	return secretName, nil
}

func handleSelfSignedCertificate(ctx context.Context, rr *odhtypes.ReconciliationRequest, secretName string, domain string) (string, error) {
	err := cluster.CreateSelfSignedCertificate(
		ctx,
		rr.Client,
		secretName,
		domain,
		GatewayNamespace,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create self-signed certificate: %w", err)
	}

	return secretName, nil
}

func createGateway(rr *odhtypes.ReconciliationRequest, certSecretName string, domain string, gatewayName string) error {
	// Input validation
	if rr == nil {
		return errors.New("reconciliation request cannot be nil")
	}
	if gatewayName == "" {
		return errors.New("gateway name cannot be empty")
	}
	if domain == "" {
		return errors.New("domain cannot be empty")
	}

	// Create listeners with validation
	listeners := CreateListeners(certSecretName, domain)

	// Create gateway resource with optimized structure
	gateway := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayName,
			Namespace: GatewayNamespace,
		},
		Spec: gwapiv1.GatewaySpec{
			GatewayClassName: gwapiv1.ObjectName(GatewayClassName),
			Listeners:        listeners,
		},
	}

	return rr.AddResources(gateway)
}

// createDestinationRule creates a DestinationRule for TLS configuration using embedded YAML template.
// This function uses embedded resources for efficient template management.
func createDestinationRule(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Input validation
	if rr == nil {
		return errors.New("reconciliation request cannot be nil")
	}

	l := logf.FromContext(ctx).WithName("createDestinationRule")
	l.V(1).Info("Creating DestinationRule for TLS configuration")

	// TODO: ass a workaround for now
	appNamespace, err := actions.ApplicationNamespace(ctx, rr)
	if err != nil {
		return fmt.Errorf("failed to get application namespace: %w", err)
	}

	yamlContent, err := gatewayResources.ReadFile("resources/destinationrule-tls.yaml")
	if err != nil {
		return fmt.Errorf("failed to read DestinationRule template: %w", err)
	}

	// Replace the APPLICATION_NAMESPACE placeholder with the actual application namespace
	yamlContent = bytes.ReplaceAll(yamlContent, []byte("APPLICATION_NAMESPACE"), []byte(appNamespace))

	decoder := serializer.NewCodecFactory(rr.Client.Scheme()).UniversalDeserializer()
	unstructuredObjects, err := resources.Decode(decoder, yamlContent)
	if err != nil {
		return fmt.Errorf("failed to decode DestinationRule YAML: %w", err)
	}

	if len(unstructuredObjects) != 2 {
		return fmt.Errorf("expected 2 DestinationRule objects, got %d", len(unstructuredObjects))
	}

	l.V(1).Info("Successfully created DestinationRule configuration")
	for i := range unstructuredObjects {
		if err := rr.AddResources(&unstructuredObjects[i]); err != nil {
			return fmt.Errorf("failed to add DestinationRule %d: %w", i, err)
		}
	}
	return nil
}

// This helper function optimizes the condition checking logic.
func isGatewayReady(gateway *gwapiv1.Gateway) bool {
	if gateway == nil {
		return false
	}
	for _, condition := range gateway.Status.Conditions {
		if condition.Type == string(gwapiv1.GatewayConditionAccepted) && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func syncGatewayConfigStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Use helper function for consistent validation
	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}

	gateway := &gwapiv1.Gateway{}
	err = rr.Client.Get(ctx, types.NamespacedName{
		Name:      DefaultGatewayName,
		Namespace: GatewayNamespace,
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

	// Use optimized helper function to check gateway readiness
	ready := isGatewayReady(gateway)

	// Determine condition values based on readiness
	conditionStatus := metav1.ConditionFalse
	reason := status.NotReadyReason
	message := status.GatewayNotReadyMessage

	if ready {
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

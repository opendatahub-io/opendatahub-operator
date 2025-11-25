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
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

//go:embed resources
var gatewayResources embed.FS

func createGatewayInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createGatewayInfrastructure")

	// Use helper function for consistent validation
	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}
	l.V(1).Info("Creating Gateway infrastructure", "gateway", gatewayConfig.Name)

	domain, err := resolveDomain(ctx, rr.Client, gatewayConfig)
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
		"certificateType", getCertificateType(gatewayConfig))

	return nil
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

	yamlContent, err := gatewayResources.ReadFile(destinationRuleTemplate)
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
			cond.SetStatusCondition(gatewayConfig, common.Condition{
				Type:    status.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  status.NotReadyReason,
				Message: status.GatewayNotFoundMessage,
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

	cond.SetStatusCondition(gatewayConfig, common.Condition{
		Type:    status.ConditionTypeReady,
		Status:  conditionStatus,
		Reason:  reason,
		Message: message,
	})

	return nil
}

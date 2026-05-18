package resources

import (
	"context"
	"errors"
	"fmt"

	fwres "github.com/opendatahub-io/operator-actions-framework/resources"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
)

const PlatformFieldOwner = fwres.PlatformFieldOwner

type ResourceSpec = fwres.ResourceSpec

var (
	ToUnstructured               = fwres.ToUnstructured
	ObjectToUnstructured         = fwres.ObjectToUnstructured
	ObjectFromUnstructured       = fwres.ObjectFromUnstructured
	Decode                       = fwres.Decode
	GvkToUnstructured            = fwres.GvkToUnstructured
	HasLabel                     = fwres.HasLabel
	SetLabels                    = fwres.SetLabels
	SetLabel                     = fwres.SetLabel
	RemoveLabel                  = fwres.RemoveLabel
	GetLabel                     = fwres.GetLabel
	HasAnnotation                = fwres.HasAnnotation
	SetAnnotations               = fwres.SetAnnotations
	SetAnnotation                = fwres.SetAnnotation
	RemoveAnnotation             = fwres.RemoveAnnotation
	GetAnnotation                = fwres.GetAnnotation
	Hash                         = fwres.Hash
	StripServerMetadata          = fwres.StripServerMetadata
	EncodeToString               = fwres.EncodeToString
	KindForObject                = fwres.KindForObject
	GetGroupVersionKindForObject = fwres.GetGroupVersionKindForObject
	EnsureGroupVersionKind       = fwres.EnsureGroupVersionKind
	NamespacedNameFromObject     = fwres.NamespacedNameFromObject
	FormatNamespacedName         = fwres.FormatNamespacedName
	FormatUnstructuredName       = fwres.FormatUnstructuredName
	FormatObjectReference        = fwres.FormatObjectReference
	RemoveOwnerReferences        = fwres.RemoveOwnerReferences
	IsOwnedByType                = fwres.IsOwnedByType
	GvkToPartial                 = fwres.GvkToPartial
	Apply                        = fwres.Apply
	ApplyStatus                  = fwres.ApplyStatus
	ListAvailableAPIResources    = fwres.ListAvailableAPIResources
	DeleteResources              = fwres.DeleteResources
	DeleteOneResource            = fwres.DeleteOneResource
	UnsetOwnerReferences         = fwres.UnsetOwnerReferences
)

// IngressHost returns the host of an admitted OpenShift Route.
func IngressHost(r routev1.Route) string {
	if len(r.Status.Ingress) != 1 {
		return ""
	}

	in := r.Status.Ingress[0]

	for i := range in.Conditions {
		if in.Conditions[i].Type == routev1.RouteAdmitted && in.Conditions[i].Status == corev1.ConditionTrue {
			return in.Host
		}
	}

	return ""
}

// GetGatewayDomain retrieves the gateway domain from GatewayConfig.Status.Domain.
func GetGatewayDomain(ctx context.Context, cli client.Client) (string, error) {
	gatewayConfig := &serviceApi.GatewayConfig{}
	gatewayConfig.SetName(serviceApi.GatewayConfigName)

	if err := cli.Get(ctx, client.ObjectKeyFromObject(gatewayConfig), gatewayConfig); err != nil {
		if k8serr.IsNotFound(err) {
			return "", errors.New("GatewayConfig not found")
		}
		return "", fmt.Errorf("failed to get GatewayConfig: %w", err)
	}

	if gatewayConfig.Status.Domain == "" {
		return "", errors.New("GatewayConfig.Status.Domain is empty")
	}

	return gatewayConfig.Status.Domain, nil
}

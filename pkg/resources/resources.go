package resources

import (
	"context"
	"fmt"

	fwres "github.com/opendatahub-io/odh-platform-utilities/framework/resources"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
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

// GetGatewayConfig retrieves the platform GatewayConfig, if it exists.
func GetGatewayConfig(ctx context.Context, cli client.Client) (*serviceApi.GatewayConfig, error) {
	gatewayConfig := &serviceApi.GatewayConfig{}
	gatewayConfig.SetName(serviceApi.GatewayConfigName)

	if err := cli.Get(ctx, client.ObjectKeyFromObject(gatewayConfig), gatewayConfig); err != nil {
		return nil, fmt.Errorf("get GatewayConfig: %w", err)
	}

	return gatewayConfig, nil
}

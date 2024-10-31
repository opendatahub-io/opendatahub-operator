package modelregistry

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	_ "embed"
)

const (
	ServiceMeshNotConfiguredReason  = "ServiceMeshNotConfigured"
	ServiceMeshNotConfiguredMessage = "ServiceMesh needs to be set to 'Managed' in DSCI CR, it is required by Model Registry"
)

func gate(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	obj, ok := rr.Instance.(odhtypes.ResourceObject)
	if !ok {
		return fmt.Errorf("resource instance %v is not a ResourceObject", rr.Instance)
	}

	if rr.DSCI.Spec.ServiceMesh != nil && rr.DSCI.Spec.ServiceMesh.ManagementState == operatorv1.Managed {
		return nil
	}

	s := obj.GetStatus()
	s.Phase = "NotReady"

	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:               status.ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             ServiceMeshNotConfiguredReason,
		Message:            ServiceMeshNotConfiguredMessage,
		ObservedGeneration: s.ObservedGeneration,
	})

	return odherrors.NewStopError(ServiceMeshNotConfiguredMessage)
}

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	c, ok := rr.Instance.(*componentsv1.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentsv1.ModelRegistry)", rr.Instance)
	}

	rr.Manifests = []odhtypes.ManifestInfo{
		baseManifestInfo(rr.Release.Name, BaseManifestsSourcePath),
		extraManifestInfo(rr.Release.Name, BaseManifestsSourcePath),
	}

	df := c.GetDevFlags()

	if df == nil {
		return nil
	}
	if len(df.Manifests) == 0 {
		return nil
	}
	if len(df.Manifests) > 1 {
		return fmt.Errorf("unexpected number of manifests found: %d, expected 1)", len(df.Manifests))
	}

	if err := odhdeploy.DownloadManifests(ctx, ComponentName, df.Manifests[0]); err != nil {
		return err
	}

	if df.Manifests[0].SourcePath != "" {
		rr.Manifests = []odhtypes.ManifestInfo{
			baseManifestInfo(rr.Release.Name, df.Manifests[0].SourcePath),
			extraManifestInfo(rr.Release.Name, df.Manifests[0].SourcePath),
		}
	}

	return nil
}

func configureDependencies(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	c, ok := rr.Instance.(*componentsv1.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentsv1.ModelRegistry)", rr.Instance)
	}

	// Namespace

	if err := rr.AddResource(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: c.Spec.RegistriesNamespace,
			},
		},
	); err != nil {
		return fmt.Errorf("failed to add namespace %s to manifests", c.Spec.RegistriesNamespace)
	}

	// Secret

	// TODO: this should be done by a dedicated controller
	is, err := cluster.FindDefaultIngressSecret(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to find default ingress secret for model registry: %w", err)
	}

	if err := rr.AddResource(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      DefaultModelRegistryCert,
				Namespace: rr.DSCI.Spec.ServiceMesh.ControlPlane.Namespace,
			},
			Data: is.Data,
			Type: is.Type,
		},
	); err != nil {
		return fmt.Errorf("failed to add default ingress secret for model registry: %w", err)
	}

	// Service Mesh

	smm, err := createServiceMeshMember(rr.DSCI, c.Spec.RegistriesNamespace)
	if err != nil {
		return fmt.Errorf("failed to create ServiceMesh Member: %w", err)
	}

	if err := rr.AddResource(smm); err != nil {
		return fmt.Errorf("failed to add ServiceMesh Member: %w", err)
	}

	return nil
}

func customizeResources(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Some ClusterRoles are part of the component deployment, but not owned by the
	// operator (overlays/odh/extras) and we expect them to be left on the cluster
	// even if the component is removed, hence we should mark them a not managed by
	// the operator. By doing so the deploy action won't set ownership and won't
	// patch them, just recreate if missing
	for i := range rr.Resources {
		r := rr.Resources[i]

		switch {
		case r.GroupVersionKind() == gvk.ClusterRole && r.GetName() == "modelregistry-editor-role":
			resources.SetAnnotation(&rr.Resources[i], annotations.ManagedByODHOperator, "false")
		case r.GroupVersionKind() == gvk.ClusterRole && r.GetName() == "modelregistry-viewer-role":
			resources.SetAnnotation(&rr.Resources[i], annotations.ManagedByODHOperator, "false")
		}
	}

	return nil
}

func updateStatus(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentsv1.ModelRegistry)
	if !ok {
		return errors.New("instance is not of type *odhTypes.ModelRegistry")
	}

	mr.Status.RegistriesNamespace = mr.Spec.RegistriesNamespace

	return nil
}

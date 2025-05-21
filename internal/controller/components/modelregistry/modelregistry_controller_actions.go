package modelregistry

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	maistrav1 "maistra.io/api/core/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelRegistry)", rr.Instance)
	}

	rr.Manifests = []odhtypes.ManifestInfo{
		baseManifestInfo(BaseManifestsSourcePath),
		extraManifestInfo(BaseManifestsSourcePath),
	}

	df := mr.GetDevFlags()

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
			baseManifestInfo(df.Manifests[0].SourcePath),
			extraManifestInfo(df.Manifests[0].SourcePath),
		}
	}

	return nil
}

func customizeManifests(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelRegistry)", rr.Instance)
	}

	// update registries namespace in manifests
	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), nil, map[string]string{
		"REGISTRIES_NAMESPACE": mr.Spec.RegistriesNamespace,
	}); err != nil {
		return fmt.Errorf("failed to update params on path %s: %w", rr.Manifests[0].String(), err)
	}
	return nil
}

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {

	if rr.DSCI.Spec.ServiceMesh == nil || rr.DSCI.Spec.ServiceMesh.ManagementState != operatorv1.Managed {
		rr.Conditions.MarkFalse(
			status.ConditionServiceMeshAvailable,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithReason(status.ServiceMeshNotConfiguredReason),
			conditions.WithMessage(status.ServiceMeshNotConfiguredMessage),
		)
		return nil
	}

	_, err := cluster.GetCRD(ctx, rr.Client, ServiceMeshMemberCRD)
	switch {
	case k8serr.IsNotFound(err):
		rr.Conditions.MarkFalse(
			status.ConditionServiceMeshAvailable,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithReason(status.ServiceMeshNotConfiguredReason),
			conditions.WithMessage(ServiceMeshMemberAPINotFound),
		)

		return ErrServiceMeshMemberAPINotFound
	case err != nil:
		return err
	}

	rr.Conditions.MarkTrue(status.ConditionServiceMeshAvailable)
	return nil
}

func configureDependencies(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelRegistry)", rr.Instance)
	}

	// Namespace
	if err := rr.AddResources(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: mr.Spec.RegistriesNamespace,
			},
		},
	); err != nil {
		return fmt.Errorf("failed to add namespace %s to manifests: %w", mr.Spec.RegistriesNamespace, err)
	}

	// Do nothing more if ServiceMesh is not Managed
	if rr.DSCI.Spec.ServiceMesh == nil || rr.DSCI.Spec.ServiceMesh.ManagementState != operatorv1.Managed {
		return nil
	}

	// To create secret only when ServiceMesh is enabled
	// TODO: this should be done by a dedicated controller
	is, err := cluster.FindDefaultIngressSecret(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to find default ingress secret for model registry: %w", err)
	}
	if err := rr.AddResources(
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

	// To add SMM only when ServiceMesh is enabled
	if err := rr.AddResources(
		&maistrav1.ServiceMeshMember{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default",
				Namespace: mr.Spec.RegistriesNamespace,
			},
			Spec: maistrav1.ServiceMeshMemberSpec{
				ControlPlaneRef: maistrav1.ServiceMeshControlPlaneRef{
					Namespace: rr.DSCI.Spec.ServiceMesh.ControlPlane.Namespace,
					Name:      rr.DSCI.Spec.ServiceMesh.ControlPlane.Name,
				},
			},
		},
	); err != nil {
		return fmt.Errorf("failed to add ServiceMeshMember resource: %w", err)
	}

	return nil
}

func updateStatus(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return errors.New("instance is not of type *odhTypes.ModelRegistry")
	}

	mr.Status.RegistriesNamespace = mr.Spec.RegistriesNamespace

	return nil
}

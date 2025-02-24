package modelregistry

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"

	_ "embed"
)

func checkPreConditions(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelRegistry", rr.Instance)
	}

	if rr.DSCI.Spec.ServiceMesh != nil && rr.DSCI.Spec.ServiceMesh.ManagementState == operatorv1.Managed {
		return nil
	}

	s := mr.GetStatus()
	s.Phase = "NotReady"

	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:               status.ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             status.ServiceMeshNotConfiguredReason,
		Message:            status.ServiceMeshNotConfiguredMessage,
		ObservedGeneration: s.ObservedGeneration,
	})

	return odherrors.NewStopError(status.ServiceMeshNotConfiguredMessage)
}

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelRegistry)", rr.Instance)
	}

	rr.Manifests = []odhtypes.ManifestInfo{
		baseManifestInfo(BaseManifestsSourcePath),
		extraManifestInfo(BaseManifestsSourcePath),
	}

	rr.Templates = []odhtypes.TemplateInfo{{
		FS:   resourcesFS,
		Path: ServiceMeshMemberTemplate,
	}}

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
		return fmt.Errorf("failed to add namespace %s to manifests", mr.Spec.RegistriesNamespace)
	}

	// Secret

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

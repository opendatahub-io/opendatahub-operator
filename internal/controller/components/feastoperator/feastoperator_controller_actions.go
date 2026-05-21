package feastoperator

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	deploymentName     = "feast-operator-controller-manager"
	selectorLabelKey   = "app.kubernetes.io/name"
	selectorLabelValue = "feast-operator"
)

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error { //nolint:unparam
	rr.Manifests = append(rr.Manifests, manifestPath(rr.ManifestsBasePath, rr.Release.Name))
	return nil
}

// migrateDeploymentSelector deletes the feast-operator-controller-manager Deployment if its
// spec.selector.matchLabels is missing the app.kubernetes.io/name label. This handles upgrades
// where the selector changed between releases — since spec.selector is immutable on Deployments,
// the only way to update it is to delete and let the operator recreate it.
func migrateDeploymentSelector(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	ns, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to determine application namespace: %w", err)
	}

	deploy := &appsv1.Deployment{}
	err = rr.Client.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: ns}, deploy)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get Deployment %s/%s: %w", ns, deploymentName, err)
	}

	if deploy.Spec.Selector == nil {
		return nil
	}

	if deploy.Spec.Selector.MatchLabels[selectorLabelKey] == selectorLabelValue {
		return nil
	}

	log.Info("Feast operator Deployment has stale selector, deleting for recreation",
		"deployment", deploymentName,
		"namespace", ns,
		"currentSelector", deploy.Spec.Selector.MatchLabels,
	)

	if err := rr.Client.Delete(ctx, deploy); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete Deployment %s/%s with stale selector: %w", ns, deploymentName, err)
	}

	log.Info("Deleted Feast operator Deployment, it will be recreated with the correct selector",
		"deployment", deploymentName, "namespace", ns)

	return nil
}

// setKustomizedParams merges runtime values from the FeastOperator CR into params.env before kustomize
// (same role as dashboard's setKustomizedParams). Add future keys to extraParams as needed.
func setKustomizedParams(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	feast, ok := rr.Instance.(*componentApi.FeastOperator)
	if !ok {
		return errors.New("instance is not a FeastOperator")
	}

	if len(rr.Manifests) == 0 {
		return errors.New("no manifests initialized before setKustomizedParams")
	}

	issuerURL := ""
	if feast.Spec.OIDC != nil {
		var err error
		issuerURL, err = parseAndValidateOIDCIssuerURL(feast.Spec.OIDC.IssuerURL)
		if err != nil {
			return fmt.Errorf("invalid OIDC issuer URL %q in FeastOperator spec.oidc: %w", feast.Spec.OIDC.IssuerURL, err)
		}
	}

	extraParams := map[string]string{
		ParamsEnvKeyOIDCIssuerURL: issuerURL,
	}

	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), "params.env", nil, extraParams); err != nil {
		return fmt.Errorf("failed to update params.env with kustomize parameters: %w", err)
	}

	return nil
}

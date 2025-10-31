package kserve

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = []odhtypes.ManifestInfo{
		kserveManifestInfo(kserveManifestSourcePath),
	}

	return nil
}

func removeOwnershipFromUnmanagedResources(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	for _, res := range rr.Resources {
		if shouldRemoveOwnerRefAndLabel(res) {
			if err := getAndRemoveOwnerReferences(ctx, rr.Client, res, isKserveOwnerRef); err != nil {
				return odherrors.NewStopErrorW(err)
			}
		}
	}

	return nil
}

func cleanUpTemplatedResources(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	logger := logf.FromContext(ctx)

	for _, res := range rr.Resources {
		if isForDependency("serverless")(&res) || isForDependency("servicemesh")(&res) {
			err := rr.Client.Delete(ctx, &res, client.PropagationPolicy(metav1.DeletePropagationForeground))
			if err != nil {
				if k8serr.IsNotFound(err) {
					continue
				}
				if meta.IsNoMatchError(err) { // when CRD is missing,
					continue
				}
				return odherrors.NewStopErrorW(err)
			}
			logger.Info("Deleted", "kind", res.GetKind(), "name", res.GetName(), "namespace", res.GetNamespace())
		}
	}

	if err := rr.RemoveResources(isForDependency("servicemesh")); err != nil {
		return odherrors.NewStopErrorW(err)
	}

	if err := rr.RemoveResources(isForDependency("serverless")); err != nil {
		return odherrors.NewStopErrorW(err)
	}

	return nil
}

func customizeKserveConfigMap(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	kserveConfigMap := corev1.ConfigMap{}
	cmidx, err := getIndexedResource(rr.Resources, &kserveConfigMap, gvk.ConfigMap, kserveConfigMapName)
	if err != nil {
		return err
	}

	//nolint:staticcheck
	serviceClusterIPNone := true
	if k.Spec.RawDeploymentServiceConfig == componentApi.KserveRawHeaded {
		// As default is Headless, only set false here if Headed is explicitly set
		serviceClusterIPNone = false
	}

	if err := updateInferenceCM(&kserveConfigMap, serviceClusterIPNone); err != nil {
		return err
	}

	if err = replaceResourceAtIndex(rr.Resources, cmidx, &kserveConfigMap); err != nil {
		return err
	}
	kserveConfigHash, err := hashConfigMap(&kserveConfigMap)
	if err != nil {
		return err
	}

	kserveDeployment := appsv1.Deployment{}
	deployidx, err := getIndexedResource(rr.Resources, &kserveDeployment, gvk.Deployment, "kserve-controller-manager")
	if err != nil {
		return err
	}
	kserveDeployment.Spec.Template.Annotations[labels.ODHAppPrefix+"/KserveConfigHash"] = kserveConfigHash

	if err = replaceResourceAtIndex(rr.Resources, deployidx, &kserveDeployment); err != nil {
		return err
	}

	return nil
}

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	rr.Conditions.MarkUnknown(status.ConditionLLMDAvailable)

	if k.Spec.LLMD.ManagementState != operatorv1.Managed {
		rr.Conditions.MarkFalse(
			status.ConditionLLMDAvailable,
			conditions.WithSeverity(common.ConditionSeverityInfo),
			conditions.WithReason(string(k.Spec.LLMD.ManagementState)),
			conditions.WithMessage("Kserve LLM-D management state is set to: %s", string(k.Spec.LLMD.ManagementState)))

		return nil
	}

	var operatorsErr error

	if found, err := cluster.OperatorExists(ctx, rr.Client, leaderWorkerSetOperator); err != nil || !found {
		if err != nil {
			return odherrors.NewStopErrorW(err)
		}

		operatorsErr = multierror.Append(operatorsErr, ErrLeaderWorkerSetOperatorNotInstalled)
	}

	if found, err := cluster.OperatorExists(ctx, rr.Client, kuadrantOperator); err != nil || !found {
		if err != nil {
			return odherrors.NewStopErrorW(err)
		}
		operatorsErr = multierror.Append(operatorsErr, ErrRHCLNotInstalled)
	}

	if operatorsErr != nil {
		rr.Conditions.MarkFalse(
			status.ConditionLLMDAvailable,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithError(operatorsErr),
		)

		return odherrors.NewStopErrorW(operatorsErr)
	}

	rr.Conditions.MarkTrue(status.ConditionLLMDAvailable)

	return nil
}

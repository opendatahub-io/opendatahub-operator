package controller

import (
	"context"
	"fmt"
	"os"

	frameworkapi "github.com/opendatahub-io/odh-platform-utilities/framework/api"
	"github.com/opendatahub-io/odh-platform-utilities/framework/controller/actions"
	"github.com/opendatahub-io/odh-platform-utilities/framework/controller/actions/deploy"
	"github.com/opendatahub-io/odh-platform-utilities/framework/controller/conditions"
	"github.com/opendatahub-io/odh-platform-utilities/framework/controller/reconciler"
	frameworktypes "github.com/opendatahub-io/odh-platform-utilities/framework/controller/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	syntheticv1alpha1 "github.com/opendatahub-io/opendatahub-operator/tests/e2e/syntheticmodule/api/v1alpha1"
)

const (
	ApplicationsNamespaceEnv = "APPLICATIONS_NAMESPACE"
	DefaultNamespace         = "redhat-ods-applications"
	PlatformConfigName       = "odh-synthetice2e-config"
	OwnedConfigMapName       = "synthetic-module-output"
	FinalizerName            = "synthetic.platform.opendatahub.io/finalizer"

	faultReleaseVersion = "fault-injected-platform-release"
)

func NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &syntheticv1alpha1.SyntheticModule{}).
		WithInstanceName(syntheticv1alpha1.SyntheticModuleInstanceName).
		WithDynamicOwnership().
		WithAction(renderOwnedConfigMap()).
		WithAction(loadPlatformVersion()).
		WithAction(markNotDegraded()).
		WithAction(injectFault()).
		WithAction(deploy.NewAction()).
		WithFinalizer(finalize()).
		WithConditions(
			conditions.Dependent(frameworkapi.ConditionTypeProvisioningSucceeded, conditions.HealthyWhenTrue),
			conditions.Dependent(frameworkapi.ConditionTypeDegraded, conditions.HealthyWhenFalse),
		).
		WithReconcilerOpts(reconciler.WithFinalizerName(FinalizerName)).
		WithPostStatusFn(updateReleaseStatus).
		Build(ctx)
	return err
}

func renderOwnedConfigMap() actions.Fn {
	return func(_ context.Context, rr *frameworktypes.ReconciliationRequest) error {
		instance, ok := rr.Instance.(*syntheticv1alpha1.SyntheticModule)
		if !ok {
			return fmt.Errorf("expected SyntheticModule, got %T", rr.Instance)
		}

		message := instance.Spec.Message
		if message == "" {
			message = "synthetic module is ready"
		}

		return rr.AddResources(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      OwnedConfigMapName,
				Namespace: applicationsNamespace(),
			},
			Data: map[string]string{"message": message},
		})
	}
}

func loadPlatformVersion() actions.Fn {
	return func(ctx context.Context, rr *frameworktypes.ReconciliationRequest) error {
		config := &corev1.ConfigMap{}
		err := rr.Client.Get(ctx, types.NamespacedName{
			Name:      PlatformConfigName,
			Namespace: applicationsNamespace(),
		}, config)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("platform config ConfigMap %s/%s is missing: %w", applicationsNamespace(), PlatformConfigName, err)
			}
			return fmt.Errorf("get platform config ConfigMap: %w", err)
		}

		version := config.Data["platformVersion"]
		if version == "" {
			return fmt.Errorf("platform config ConfigMap %s/%s has no platformVersion", applicationsNamespace(), PlatformConfigName)
		}
		if rr.Extensions == nil {
			rr.Extensions = make(map[string]any)
		}
		rr.Extensions["synthetic.platformVersion"] = version
		return nil
	}
}

func markNotDegraded() actions.Fn {
	return func(_ context.Context, rr *frameworktypes.ReconciliationRequest) error {
		rr.Conditions.MarkFalse(
			frameworkapi.ConditionTypeDegraded,
			conditions.WithReason("NoWarnings"),
			conditions.WithMessage("No degraded conditions detected"),
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
		)
		return nil
	}
}

func injectFault() actions.Fn {
	return func(_ context.Context, rr *frameworktypes.ReconciliationRequest) error {
		switch faultMode(rr.Instance) {
		case syntheticv1alpha1.FaultModeReadyFalse:
			return fmt.Errorf("fault injection %q requested", syntheticv1alpha1.FaultModeReadyFalse)
		case syntheticv1alpha1.FaultModeDegraded:
			rr.Conditions.MarkTrue(
				frameworkapi.ConditionTypeDegraded,
				conditions.WithReason("FaultInjected"),
				conditions.WithMessage("degraded condition requested by fault annotation"),
				conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			)
		}
		return nil
	}
}

func finalize() actions.Fn {
	return func(_ context.Context, rr *frameworktypes.ReconciliationRequest) error {
		if faultMode(rr.Instance) == syntheticv1alpha1.FaultModeHoldFinalizer {
			return fmt.Errorf("remove annotation %q to allow finalization", syntheticv1alpha1.FaultAnnotationKey)
		}
		return nil
	}
}

func updateReleaseStatus(_ context.Context, rr *frameworktypes.ReconciliationRequest, happy bool) error {
	if !happy {
		return nil
	}

	instance, ok := rr.Instance.(*syntheticv1alpha1.SyntheticModule)
	if !ok {
		return fmt.Errorf("expected SyntheticModule, got %T", rr.Instance)
	}
	version, ok := rr.Extensions["synthetic.platformVersion"].(string)
	if !ok || version == "" {
		return fmt.Errorf("platform version was not loaded")
	}

	if faultMode(instance) == syntheticv1alpha1.FaultModeStalePlatformRelease {
		version = faultReleaseVersion
	}
	instance.Status.SetRelease(frameworkapi.ComponentRelease{
		Name:    frameworkapi.ReleasePlatform,
		Version: version,
		RepoURL: "https://github.com/opendatahub-io/opendatahub-operator",
	})
	if faultMode(instance) == syntheticv1alpha1.FaultModeStaleGeneration {
		instance.Status.ObservedGeneration = instance.GetGeneration() - 1
	}
	return nil
}

func faultMode(instance metav1.Object) string {
	return instance.GetAnnotations()[syntheticv1alpha1.FaultAnnotationKey]
}

func applicationsNamespace() string {
	if namespace := os.Getenv(ApplicationsNamespaceEnv); namespace != "" {
		return namespace
	}
	return DefaultNamespace
}

package modules

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhan "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	odhl "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// PlatformInputs are cluster values shared by module handlers.
type PlatformInputs struct {
	ApplicationsNamespace string
	MonitoringNamespace   string
}

// ResolvePlatformInputs resolves shared platform values once per reconcile.
// Applications namespace is required. Monitoring may not exist yet,
// especially on xKS clusters.
func ResolvePlatformInputs(ctx context.Context, cli client.Client) (PlatformInputs, error) {
	applicationsNamespace, err := cluster.ApplicationNamespace(ctx, cli)
	if err != nil {
		return PlatformInputs{}, fmt.Errorf("resolving applications namespace: %w", err)
	}

	inputs := PlatformInputs{ApplicationsNamespace: applicationsNamespace}
	if monitoringNamespace, monitoringErr := cluster.MonitoringNamespace(ctx, cli); monitoringErr == nil {
		inputs.MonitoringNamespace = monitoringNamespace
	}

	return inputs, nil
}

// BuildPlatformModulesForSource projects only handlers owned by source into
// PlatformModules. Other source fields remain untouched for SSA ownership.
func BuildPlatformModulesForSource(dscCtx *DSCContext, source ConfigSource) configApi.PlatformModules {
	var modules configApi.PlatformModules
	_ = DefaultRegistry().ForConfigSource(source, func(handler ModuleHandler, _ bool) error {
		handler.PopulatePlatformModule(&modules, dscCtx)
		return nil
	})
	return modules
}

// NewPlatformCR builds source-specific Platform desired state. DSC and DSCI
// controllers apply separate fields with separate SSA field managers.
func NewPlatformCR(dscCtx *DSCContext, source ConfigSource) *configApi.Platform {
	return &configApi.Platform{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configApi.GroupVersion.String(),
			Kind:       configApi.PlatformKind,
		},
		ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName},
		Spec: configApi.PlatformSpec{
			Modules: BuildPlatformModulesForSource(dscCtx, source),
		},
	}
}

// NewPlatformCRRemovedForSource builds a source-specific delete projection.
// Every registered handler owned by source is explicitly set Removed so its
// PlatformModule tracker can be deleted. Fields owned by other sources stay
// absent and are preserved by SSA.
func NewPlatformCRRemovedForSource(dscCtx *DSCContext, source ConfigSource) *configApi.Platform {
	platform := NewPlatformCR(dscCtx, source)
	_ = DefaultRegistry().ForConfigSource(source, func(handler ModuleHandler, _ bool) error {
		setPlatformModuleState(&platform.Spec.Modules, handler.GetName(), string(operatorv1.Removed))
		return nil
	})
	return platform
}

func setPlatformModuleState(modules *configApi.PlatformModules, moduleName, state string) {
	if modules == nil {
		return
	}
	v := reflect.ValueOf(modules).Elem()
	t := v.Type()
	for i := range v.NumField() {
		field := t.Field(i)
		jsonName, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if jsonName == moduleName || strings.EqualFold(field.Name, moduleName) {
			managementState := v.Field(i).FieldByName("ManagementState")
			if managementState.IsValid() && managementState.CanSet() {
				managementState.SetString(state)
			}
			return
		}
	}
}

// SetPlatformMetadata adds source identity metadata while preserving fields
// written by the other platform source/controller.
func SetPlatformMetadata(platform client.Object, instance client.Object, release common.Release, partOf string) {
	annotations := platform.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[odhan.InstanceGeneration] = strconv.FormatInt(instance.GetGeneration(), 10)
	annotations[odhan.InstanceName] = instance.GetName()
	annotations[odhan.InstanceUID] = string(instance.GetUID())
	annotations[odhan.PlatformType] = string(release.Name)
	annotations[odhan.PlatformVersion] = release.Version.String()
	platform.SetAnnotations(annotations)

	labels := platform.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[odhl.PlatformPartOf] = odhl.NormalizePartOfValue(partOf)
	platform.SetLabels(labels)
}

// EnsurePlatformOwnerReference merges instance as a non-controller owner of
// Platform. Platform can have both DSC and DSCI owners; conflict retries keep
// concurrent SSA updates safe.
func EnsurePlatformOwnerReference(ctx context.Context, cli client.Client, owner client.Object, scheme *runtime.Scheme) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		platform := &configApi.Platform{}
		if err := cli.Get(ctx, client.ObjectKey{Name: configApi.PlatformInstanceName}, platform); err != nil {
			return err
		}

		// Never attempt to resurrect a Platform that Kubernetes has already
		// marked for deletion. Its owners may be changing as part of teardown.
		if !platform.GetDeletionTimestamp().IsZero() {
			return nil
		}

		ownerReferences := append([]metav1.OwnerReference(nil), platform.GetOwnerReferences()...)
		if err := controllerutil.SetOwnerReference(owner, platform, scheme); err != nil {
			return fmt.Errorf("failed to merge Platform owner reference: %w", err)
		}
		if reflect.DeepEqual(ownerReferences, platform.GetOwnerReferences()) {
			return nil
		}

		if err := cli.Update(ctx, platform); err != nil {
			return err
		}
		return nil
	})
}

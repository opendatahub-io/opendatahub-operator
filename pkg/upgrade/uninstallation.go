package upgrade

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	// DeleteConfigMapLabel is the label for configMap used to trigger operator uninstall
	// TODO: Label should be updated if addon name changes.
	DeleteConfigMapLabel = "api.openshift.com/addon-managed-odh-delete"

	odhOperatorPackage   = "opendatahub-operator"
	rhoaiOperatorPackage = "rhods-operator"
)

// OperatorUninstall deletes all the externally generated resources.
// This includes DSCI, namespaces created by the operator (but not workbench or MR's),
// Subscription/CSV (OLM v0), and matching Catalog ClusterExtension (OLM v1).
func OperatorUninstall(ctx context.Context, cli client.Client, platform common.Platform) error {
	log := logf.FromContext(ctx)

	if err := ensureLLMISVCCleanupReady(ctx, cli); err != nil {
		return err
	}

	if err := removeDSC(ctx, cli); err != nil {
		return err
	}

	if err := removeDSCI(ctx, cli); err != nil {
		return err
	}

	// Delete generated namespaces by the operator
	generatedNamespaces := &corev1.NamespaceList{}
	nsOptions := []client.ListOption{
		client.MatchingLabels{labels.ODH.OwnedNamespace: "true"},
	}
	if err := cli.List(ctx, generatedNamespaces, nsOptions...); err != nil {
		return fmt.Errorf("error getting generated namespaces : %w", err)
	}

	// Return if any one of the namespaces is Terminating due to resources that are in process of deletion. (e.g. CRDs)
	for _, namespace := range generatedNamespaces.Items {
		if namespace.Status.Phase == corev1.NamespaceTerminating {
			return fmt.Errorf("waiting for namespace %v to be deleted", namespace.Name)
		}
	}

	for _, namespace := range generatedNamespaces.Items {
		if namespace.Status.Phase == corev1.NamespaceActive {
			if err := cli.Delete(ctx, &namespace); err != nil {
				return fmt.Errorf("error deleting namespace %v: %w", namespace.Name, err)
			}
			log.Info("Namespace deleted as a part of uninstallation", "namespace", namespace.Name)
		}
	}

	// give enough time for namespace deletion before proceed
	time.Sleep(10 * time.Second)

	// We can only assume the subscription is using standard names
	// if user install by creating different named subs, then we will not know the name
	// we cannot remove CSV before remove subscription because that need SA account
	operatorNs, err := cluster.GetOperatorNamespace()
	if err != nil {
		return err
	}

	log.Info("Removing operator subscription which in turn will remove installplan")
	subsName := "opendatahub-operator"
	if platform == cluster.SelfManagedRhoai {
		subsName = "rhods-operator"
	}
	if platform != cluster.ManagedRhoai {
		if err := cluster.DeleteExistingSubscription(ctx, cli, operatorNs, subsName); err != nil {
			return err
		}
	}

	// CSV (OLMv0) and ClusterExtension (OLMv1) both tear down the operator
	// Deployment and its SA/RBAC. Keep them last, CE after CSV, so earlier
	// uninstall steps still have a live operator identity.
	log.Info("Removing the operator CSV in turn remove operator deployment")
	if err := removeCSV(ctx, cli); err != nil {
		return err
	}

	if err := removeClusterExtension(ctx, cli, platform, operatorNs); err != nil {
		return err
	}

	log.Info("All resources deleted as part of uninstall.")
	return nil
}

// ensureLLMISVCCleanupReady blocks DSC deletion while LLMInferenceServices
// remain or their controller has not finished clearing config usage status.
// This keeps the KServe controller running while users delete their services
// and normal finalization releases LLMInferenceServiceConfig references.
// Neither services nor configs are deleted here: their normal finalizers remain
// authoritative and uninstall only observes their progress.
func ensureLLMISVCCleanupReady(ctx context.Context, cli client.Client) error {
	llmInferenceServiceGVKs := []schema.GroupVersionKind{
		gvk.LLMInferenceServiceV1Alpha2,
		gvk.LLMInferenceServiceV1Alpha1,
	}

	var blockers []string
	seenServices := map[client.ObjectKey]struct{}{}
	for _, serviceGVK := range llmInferenceServiceGVKs {
		services, err := listLLMISVCResources(ctx, cli, serviceGVK)
		if err != nil {
			return err
		}
		for i := range services {
			key := client.ObjectKeyFromObject(&services[i])
			if _, seen := seenServices[key]; seen {
				continue
			}
			seenServices[key] = struct{}{}
			blockers = append(blockers, fmt.Sprintf("LLMInferenceService %s", key))
		}
	}

	configGVKs := []schema.GroupVersionKind{
		gvk.LLMInferenceServiceConfigV1Alpha2,
		gvk.LLMInferenceServiceConfigV1Alpha1,
	}
	seenConfigs := map[client.ObjectKey]struct{}{}
	for _, configGVK := range configGVKs {
		configs, err := listLLMISVCResources(ctx, cli, configGVK)
		if err != nil {
			return err
		}
		for i := range configs {
			key := client.ObjectKeyFromObject(&configs[i])
			if _, seen := seenConfigs[key]; seen {
				continue
			}
			seenConfigs[key] = struct{}{}

			blocker, err := llmISVCConfigCleanupBlocker(&configs[i])
			if err != nil {
				return fmt.Errorf("inspect LLMInferenceServiceConfig %s: %w", key, err)
			}
			if blocker != "" {
				blockers = append(blockers, fmt.Sprintf("LLMInferenceServiceConfig %s (%s)", key, blocker))
			}
		}
	}

	if len(blockers) == 0 {
		return nil
	}

	sort.Strings(blockers)
	return fmt.Errorf("LLMInferenceService cleanup must complete before uninstall: %s", strings.Join(blockers, ", "))
}

func listLLMISVCResources(
	ctx context.Context,
	cli client.Client,
	resourceGVK schema.GroupVersionKind,
) ([]unstructured.Unstructured, error) {
	resources := &unstructured.UnstructuredList{}
	resources.SetGroupVersionKind(resourceGVK)
	if err := cli.List(ctx, resources); err != nil {
		if meta.IsNoMatchError(err) || k8serr.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list %s resources: %w", resourceGVK, err)
	}

	return resources.Items, nil
}

func llmISVCConfigCleanupBlocker(config *unstructured.Unstructured) (string, error) {
	if !config.GetDeletionTimestamp().IsZero() {
		return "terminating", nil
	}

	observedGeneration, found, err := unstructured.NestedInt64(config.Object, "status", "observedGeneration")
	if err != nil {
		return "", err
	}
	if !found || observedGeneration != config.GetGeneration() {
		return "waiting for llmisvc controller to observe the current generation", nil
	}

	references, found, err := unstructured.NestedSlice(config.Object, "status", "referencedBy")
	if err != nil {
		return "", err
	}
	if found && len(references) > 0 {
		var names []string
		for _, reference := range references {
			ref, ok := reference.(map[string]any)
			if !ok {
				continue
			}
			name, _ := ref["name"].(string)
			if name == "" {
				continue
			}
			namespace, _ := ref["namespace"].(string)
			if namespace == "" {
				names = append(names, name)
			} else {
				names = append(names, namespace+"/"+name)
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			return "status.referencedBy is not empty", nil
		}
		return "referenced by " + strings.Join(names, ", "), nil
	}

	conditions, found, err := unstructured.NestedSlice(config.Object, "status", "conditions")
	if err != nil {
		return "", err
	}
	if !found {
		return "waiting for ConfigInUse condition", nil
	}
	for _, condition := range conditions {
		conditionMap, ok := condition.(map[string]any)
		if !ok {
			return "", fmt.Errorf("ConfigInUse condition has invalid type %T", condition)
		}
		conditionType, _ := conditionMap["type"].(string)
		if conditionType != "ConfigInUse" {
			continue
		}
		status, ok := conditionMap["status"].(string)
		if !ok {
			return "", fmt.Errorf("ConfigInUse condition has invalid status type %T", conditionMap["status"])
		}
		if status == "False" {
			return "", nil
		}
		return "ConfigInUse=" + status, nil
	}

	return "waiting for ConfigInUse condition", nil
}

func removeDSCI(ctx context.Context, cli client.Client) error {
	instance := &dsciv2.DSCInitialization{}

	if err := cli.DeleteAllOf(ctx, instance, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		return fmt.Errorf("failure deleting DSCI: %w", err)
	}

	return nil
}

func removeDSC(ctx context.Context, cli client.Client) error {
	log := logf.FromContext(ctx)
	instance := &dscApi.DataScienceCluster{}

	// Foreground waits for DSC-owned objects, including module CRs whose
	// finalizers are processed by out-of-tree module operators. Those
	// operators stay up because the Platform CR finalizer
	// (waitForModuleCRDeletion) blocks Platform deletion — and therefore
	// GC of module-operator Deployments — until every module CR is gone.
	if err := cli.DeleteAllOf(ctx, instance, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		return fmt.Errorf("failure deleting DSC: %w", err)
	}

	// The DSCI validating webhook denies DSCI deletion while any DSC still exists.
	// Wait for all DSC objects to be fully removed before returning.
	backoff := wait.Backoff{
		Duration: 2 * time.Second,
		Factor:   2.0,
		Steps:    7,
	}

	var remainingNames []string

	if err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		dscList := &dscApi.DataScienceClusterList{}
		if err := cli.List(ctx, dscList); err != nil {
			return false, err
		}
		if len(dscList.Items) == 0 {
			return true, nil
		}
		remainingNames = make([]string, len(dscList.Items))
		for i := range dscList.Items {
			remainingNames[i] = dscList.Items[i].Name
		}
		log.Info("Waiting for DataScienceCluster objects to be deleted", "remaining", remainingNames)
		return false, nil
	}); err != nil {
		return fmt.Errorf("failure waiting for DSC deletion, remaining objects %v: %w", remainingNames, err)
	}

	return nil
}

// HasDeleteConfigMap returns true if delete configMap is added to the operator namespace by managed-tenants repo.
// It returns false in all other cases.
func HasDeleteConfigMap(ctx context.Context, c client.Client) bool {
	// Get watchNamespace
	operatorNamespace, err := cluster.GetOperatorNamespace()
	if err != nil {
		return false
	}

	// If delete configMap is added, uninstall the operator and the resources
	deleteConfigMapList := &corev1.ConfigMapList{}
	cmOptions := []client.ListOption{
		client.InNamespace(operatorNamespace),
		client.MatchingLabels{DeleteConfigMapLabel: "true"},
	}

	if err := c.List(ctx, deleteConfigMapList, cmOptions...); err != nil {
		return false
	}

	return len(deleteConfigMapList.Items) != 0
}

func removeCSV(ctx context.Context, c client.Client) error {
	log := logf.FromContext(ctx)
	// Get watchNamespace
	operatorNamespace, err := cluster.GetOperatorNamespace()
	if err != nil {
		return err
	}

	operatorCsv, err := cluster.GetClusterServiceVersion(ctx, c, operatorNamespace)
	if k8serr.IsNotFound(err) {
		ctrl.Log.Info("No clusterserviceversion for the operator found.")
		return nil
	}

	if err != nil {
		return err
	}

	log.Info("Deleting CSV", "name", operatorCsv.Name)
	err = c.Delete(ctx, operatorCsv)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("error deleting clusterserviceversion: %w", err)
	}
	log.Info("Clusterserviceversion deleted as a part of uninstall", "name", operatorCsv.Name)

	return nil
}

// removeClusterExtension deletes Catalog ClusterExtensions for the platform operator package
// that target installNamespace (spec.namespace). Matching mirrors
// odh-platform-utilities cluster.ClusterExtensionInstallsPackage scoping so an
// unrelated CE for the same package in another namespace is left alone.
// Skipped when platform is empty (unknown package) or ManagedRhoai (addon-managed).
// Absent ClusterExtension CRD is treated as success.
func removeClusterExtension(ctx context.Context, cli client.Client, platform common.Platform, installNamespace string) error {
	if platform == "" || platform == cluster.ManagedRhoai {
		return nil
	}

	packageName := odhOperatorPackage
	if platform == cluster.SelfManagedRhoai {
		packageName = rhoaiOperatorPackage
	}

	log := logf.FromContext(ctx)
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.ClusterExtension)

	if err := cli.List(ctx, list); err != nil {
		if meta.IsNoMatchError(err) || k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("list ClusterExtensions: %w", err)
	}

	for i := range list.Items {
		ext := &list.Items[i]
		if !clusterExtensionMatchesPackage(ext, packageName, installNamespace) {
			continue
		}

		log.Info("Deleting ClusterExtension as a part of uninstall",
			"name", ext.GetName(), "package", packageName, "namespace", installNamespace)
		if err := cli.Delete(ctx, ext); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete ClusterExtension %s: %w", ext.GetName(), err)
		}
	}

	return nil
}

// clusterExtensionMatchesPackage reports whether ext is a Catalog ClusterExtension
// for packageName installed into installNamespace. Same filter as
// odh-platform-utilities' unexported clusterExtensionInstallsPackage when
// installNamespace is non-empty.
func clusterExtensionMatchesPackage(ext *unstructured.Unstructured, packageName, installNamespace string) bool {
	if installNamespace != "" {
		ns, found, err := unstructured.NestedString(ext.Object, "spec", "namespace")
		if err != nil || !found || ns != installNamespace {
			return false
		}
	}

	sourceType, found, err := unstructured.NestedString(ext.Object, "spec", "source", "sourceType")
	if err != nil || !found || sourceType != "Catalog" {
		return false
	}

	pkg, found, err := unstructured.NestedString(ext.Object, "spec", "source", "catalog", "packageName")
	if err != nil || !found || pkg != packageName {
		return false
	}

	return true
}

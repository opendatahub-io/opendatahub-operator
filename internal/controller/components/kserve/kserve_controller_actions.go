package kserve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	LLMInferenceServiceConfigWellKnownAnnotationKey   = "serving.kserve.io/well-known-config"
	LLMInferenceServiceConfigWellKnownAnnotationValue = "true"

	modelCacheLabelKey   = "kserve/localmodel"
	modelCacheLabelValue = "worker"
)

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	sourcePath := kserveManifestSourcePath
	if cluster.GetClusterInfo().Type == cluster.ClusterTypeKubernetes {
		sourcePath = kserveManifestSourcePathXKS
	}

	rr.Manifests = []odhtypes.ManifestInfo{
		kserveManifestInfo(rr.ManifestsBasePath, sourcePath),
		{
			Path:       rr.ManifestsBasePath,
			ContextDir: "connectionAPI",
		},
	}

	if k.Spec.ModelCache != nil && k.Spec.ModelCache.ManagementState == operatorv1.Managed {
		rr.Manifests = append(rr.Manifests, kserveManifestInfo(rr.ManifestsBasePath, kserveManifestSourcePathModelCache))
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

func customizeKserveConfigMap(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	//nolint:staticcheck
	serviceClusterIPNone := true
	if k.Spec.RawDeploymentServiceConfig == componentApi.KserveRawHeaded {
		// As default is Headless, only set false here if Headed is explicitly set
		serviceClusterIPNone = false
	}

	// Update ConfigMap (required for both OpenShift and XKS)
	kserveConfigMap := corev1.ConfigMap{}
	cmidx, err := getIndexedResource(rr.Resources, &kserveConfigMap, gvk.ConfigMap, kserveConfigMapName)
	if err != nil {
		return err
	}

	modelCacheEnabled := k.Spec.ModelCache != nil && k.Spec.ModelCache.ManagementState == operatorv1.Managed

	if err := updateInferenceCM(&kserveConfigMap, serviceClusterIPNone, modelCacheEnabled); err != nil {
		return err
	}

	if err = replaceResourceAtIndex(rr.Resources, cmidx, &kserveConfigMap); err != nil {
		return err
	}

	// Check if kserve-controller-manager deployment exists in resources
	// If not (e.g., XKS manifests), skip hash annotation
	kserveDeployment := appsv1.Deployment{}
	deployidx, err := getIndexedResource(rr.Resources, &kserveDeployment, gvk.Deployment, isvcControllerDeployment)
	if err != nil {
		// Only skip if deployment not found; propagate other errors
		if errors.Is(err, ErrResourceNotFound) {
			return nil
		}
		return err
	}

	// Add hash annotation to deployment to trigger restart on ConfigMap changes
	kserveConfigHash, err := hashConfigMap(&kserveConfigMap)
	if err != nil {
		return err
	}
	kserveDeployment.Spec.Template.Annotations[labels.ODHAppPrefix+"/KserveConfigHash"] = kserveConfigHash

	if err = replaceResourceAtIndex(rr.Resources, deployidx, &kserveDeployment); err != nil {
		return err
	}

	return nil
}

func versionedWellKnownLLMInferenceServiceConfigs(_ context.Context, version string, rr *odhtypes.ReconciliationRequest) error {
	const (
		envFormat = "%s-kserve-"
		envName   = "LLM_INFERENCE_SERVICE_CONFIG_PREFIX"
	)

	for i := range rr.Resources {
		if rr.Resources[i].GroupVersionKind().Group == gvk.LLMInferenceServiceConfigV1Alpha1.Group &&
			rr.Resources[i].GroupVersionKind().Kind == gvk.LLMInferenceServiceConfigV1Alpha1.Kind {
			if v, ok := rr.Resources[i].GetAnnotations()[LLMInferenceServiceConfigWellKnownAnnotationKey]; ok && v == LLMInferenceServiceConfigWellKnownAnnotationValue {
				rr.Resources[i].SetName(fmt.Sprintf("%s-%s", version, rr.Resources[i].GetName()))
			}
		}

		if rr.Resources[i].GroupVersionKind().Group == gvk.Deployment.Group &&
			rr.Resources[i].GroupVersionKind().Kind == gvk.Deployment.Kind {
			deployment := &appsv1.Deployment{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(rr.Resources[i].Object, deployment); err != nil {
				return err
			}

			for j := range deployment.Spec.Template.Spec.Containers {
				container := &deployment.Spec.Template.Spec.Containers[j]
				envVarFound := false

				for k := range container.Env {
					if container.Env[k].Name == envName {
						container.Env[k].Value = fmt.Sprintf(envFormat, version)
						envVarFound = true
						break
					}
				}

				if !envVarFound {
					container.Env = append(container.Env, corev1.EnvVar{
						Name:  envName,
						Value: fmt.Sprintf(envFormat, version),
					})
				}
			}

			u, err := resources.ToUnstructured(deployment)
			if err != nil {
				return err
			}
			rr.Resources[i] = *u
		}
	}
	return nil
}

func checkOperatorAndCRDDependencies() actions.Fn {
	return dependency.NewAction(
		dependency.MonitorOperator(dependency.OperatorConfig{
			OperatorGVK: gvk.LeaderWorkerSetOperatorV1,
			Severity:    common.ConditionSeverityInfo,
			Filter:      lwsConditionFilter,
		}),
		// networking.istio.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.DestinationRule,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.EnvoyFilter,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.IstioGateway,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.ProxyConfig,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.ServiceEntry,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.Sidecar,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.WorkloadEntry,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.WorkloadGroup,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		// security.istio.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.AuthorizationPolicy,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.PeerAuthentication,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.RequestAuthentication,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		// telemetry.istio.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.Telemetry,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		// extensions.istio.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.WasmPlugin,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		// cert-manager.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.CertManagerCertificate,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.CertManagerCertificateRequest,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.CertManagerIssuer,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.CertManagerClusterIssuer,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
		// leaderworkerset.x-k8s.io.
		dependency.MonitorCRD(dependency.CRDConfig{
			GVK:          gvk.LeaderWorkerSetV1,
			ClusterTypes: []string{cluster.ClusterTypeKubernetes},
		}),
	)
}

func checkSubscriptionDependencies() actions.Fn {
	return dependency.NewSubscriptionAction(
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: LLMInferenceServiceDependencies,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: rhclOperatorSubscription, DisplayName: "Red Hat Connectivity Link"},
				{Name: certManagerOperatorSubscription, DisplayName: "cert-manager operator"},
			},
			ClusterTypes: []string{cluster.ClusterTypeOpenShift},
			Reason:       subNotFound,
			Message:      "Warning: %s not installed, LLMInferenceService cannot be used",
			Severity:     common.ConditionSeverityInfo,
		}),
		dependency.CheckSubscriptionGroup(dependency.SubscriptionGroupConfig{
			ConditionType: LLMInferenceServiceWideEPDependencies,
			Subscriptions: []dependency.SubscriptionDependency{
				{Name: rhclOperatorSubscription, DisplayName: "Red Hat Connectivity Link"},
				{Name: lwsOperatorSubscription, DisplayName: "LeaderWorkerSet"},
				{Name: certManagerOperatorSubscription, DisplayName: "cert-manager operator"},
			},
			ClusterTypes: []string{cluster.ClusterTypeOpenShift},
			Reason:       subNotFound,
			Message:      "Warning: %s not installed, Wide Expert Parallelism with LLMInferenceService cannot be used",
			Severity:     common.ConditionSeverityInfo,
		}),
	)
}

func createModelCachePVAndPVC(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	if k.Spec.ModelCache.CacheSize == nil {
		return errors.New("cacheSize is required when ModelCache is Managed")
	}

	cacheSize := *k.Spec.ModelCache.CacheSize

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kserve-localmodelnode-pv",
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, rr.Client, pv, func() error {
		pv.Spec = corev1.PersistentVolumeSpec{
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: cacheSize},
			VolumeMode:                    ptr.To(corev1.PersistentVolumeFilesystem),
			AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			StorageClassName:              "local-storage",
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/var/lib/kserve/models",
					Type: ptr.To(corev1.HostPathDirectoryOrCreate),
				},
			},
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key:      modelCacheLabelKey,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{modelCacheLabelValue},
						}},
					}},
				},
			},
		}
		return controllerutil.SetControllerReference(k, pv, rr.Client.Scheme())
	})
	if err != nil {
		return fmt.Errorf("failed to create/update model cache PV: %w", err)
	}

	// Create/update PVC
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kserve-localmodelnode-pvc",
			Namespace: cluster.GetApplicationNamespace(),
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, rr.Client, pvc, func() error {
		// Only set immutable fields on create (when VolumeName is empty).
		// On update, these fields are rejected by the API server.
		if pvc.Spec.VolumeName == "" {
			pvc.Spec.VolumeName = "kserve-localmodelnode-pv"
			pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
			pvc.Spec.VolumeMode = ptr.To(corev1.PersistentVolumeFilesystem)
			pvc.Spec.StorageClassName = ptr.To("local-storage")
		}
		pvc.Spec.Resources = corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceStorage: cacheSize},
		}
		return controllerutil.SetControllerReference(k, pvc, rr.Client.Scheme())
	})
	if err != nil {
		return fmt.Errorf("failed to create/update model cache PVC: %w", err)
	}

	return nil
}

func createLocalModelNodeGroup(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	if k.Spec.ModelCache.CacheSize == nil {
		return errors.New("cacheSize is required when ModelCache is Managed")
	}

	cacheSizeStr := k.Spec.ModelCache.CacheSize.String()

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.LocalModelNodeGroup)
	obj.SetName("workers")

	_, err := controllerutil.CreateOrUpdate(ctx, rr.Client, obj, func() error {
		obj.Object["spec"] = map[string]interface{}{
			"storageLimit": cacheSizeStr,
			"persistentVolumeSpec": map[string]interface{}{
				"capacity": map[string]interface{}{
					"storage": cacheSizeStr,
				},
				"volumeMode":                    "Filesystem",
				"accessModes":                   []interface{}{"ReadWriteOnce"},
				"persistentVolumeReclaimPolicy": "Delete",
				"storageClassName":              "local-storage",
				"hostPath": map[string]interface{}{
					"path": "/var/lib/kserve/models",
					"type": "DirectoryOrCreate",
				},
				"nodeAffinity": map[string]interface{}{
					"required": map[string]interface{}{
						"nodeSelectorTerms": []interface{}{
							map[string]interface{}{
								"matchExpressions": []interface{}{
									map[string]interface{}{
										"key":      modelCacheLabelKey,
										"operator": "In",
										"values":   []interface{}{modelCacheLabelValue},
									},
								},
							},
						},
					},
				},
			},
			"persistentVolumeClaimSpec": map[string]interface{}{
				"accessModes": []interface{}{"ReadWriteOnce"},
				"volumeMode":  "Filesystem",
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"storage": cacheSizeStr,
					},
				},
				"storageClassName": "local-storage",
			},
		}
		return controllerutil.SetControllerReference(k, obj, rr.Client.Scheme())
	})
	if err != nil {
		return fmt.Errorf("failed to create/update LocalModelNodeGroup: %w", err)
	}

	return nil
}

func labelModelCacheNodes(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	logger := logf.FromContext(ctx)

	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	var nodes []corev1.Node

	switch {
	case len(k.Spec.ModelCache.NodeNames) > 0:
		for _, name := range k.Spec.ModelCache.NodeNames {
			node := corev1.Node{}
			if err := rr.Client.Get(ctx, client.ObjectKey{Name: name}, &node); err != nil {
				return fmt.Errorf("failed to get node %q: %w", name, err)
			}
			nodes = append(nodes, node)
		}
	case k.Spec.ModelCache.NodeSelector != nil:
		sel, err := metav1.LabelSelectorAsSelector(k.Spec.ModelCache.NodeSelector)
		if err != nil {
			return fmt.Errorf("failed to convert nodeSelector to selector: %w", err)
		}
		nodeList := &corev1.NodeList{}
		if err := rr.Client.List(ctx, nodeList, client.MatchingLabelsSelector{Selector: sel}); err != nil {
			return fmt.Errorf("failed to list nodes matching selector: %w", err)
		}
		nodes = nodeList.Items
	}

	desiredNodes := make(map[string]struct{}, len(nodes))
	for i := range nodes {
		node := &nodes[i]
		desiredNodes[node.Name] = struct{}{}
		if node.Labels[modelCacheLabelKey] == modelCacheLabelValue {
			continue
		}
		original := node.DeepCopy()
		if node.Labels == nil {
			node.Labels = make(map[string]string)
		}
		node.Labels[modelCacheLabelKey] = modelCacheLabelValue
		if err := rr.Client.Patch(ctx, node, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("failed to label node %q: %w", node.Name, err)
		}
		logger.Info("Labeled node for model cache", "node", node.Name)
	}

	allLabeled := &corev1.NodeList{}
	if err := rr.Client.List(ctx, allLabeled, client.MatchingLabels{modelCacheLabelKey: modelCacheLabelValue}); err != nil {
		return fmt.Errorf("failed to list labeled nodes: %w", err)
	}
	for i := range allLabeled.Items {
		node := &allLabeled.Items[i]
		if _, desired := desiredNodes[node.Name]; !desired {
			original := node.DeepCopy()
			delete(node.Labels, modelCacheLabelKey)
			if err := rr.Client.Patch(ctx, node, client.MergeFrom(original)); err != nil {
				return fmt.Errorf("failed to unlabel node %q: %w", node.Name, err)
			}
			logger.Info("Removed stale model cache label from node", "node", node.Name)
		}
	}

	return nil
}

func reconcileModelCache(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	if k.Spec.ModelCache == nil || k.Spec.ModelCache.ManagementState != operatorv1.Managed {
		return cleanupModelCache(ctx, rr)
	}

	if err := updateNamespacePSA(ctx, rr.Client, "privileged"); err != nil {
		return err
	}

	if err := forceReconcileKserveAgentImage(ctx, rr); err != nil {
		return err
	}

	if err := createModelCachePVAndPVC(ctx, rr); err != nil {
		return err
	}

	if err := createLocalModelNodeGroup(ctx, rr); err != nil {
		return err
	}

	return labelModelCacheNodes(ctx, rr)
}

func deleteIfExists(ctx context.Context, cli client.Client, obj client.Object, description string) error {
	key := client.ObjectKeyFromObject(obj)
	if err := cli.Get(ctx, key, obj); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("failed to check %s %s: %w", description, key, err)
	}
	if err := cli.Delete(ctx, obj); err != nil && !k8serr.IsNotFound(err) {
		return fmt.Errorf("failed to delete %s %s: %w", description, key, err)
	}
	return nil
}

func cleanupModelCache(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	logger := logf.FromContext(ctx)

	if err := updateNamespacePSA(ctx, rr.Client, "baseline"); err != nil {
		return err
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kserve-localmodelnode-pvc",
			Namespace: cluster.GetApplicationNamespace(),
		},
	}
	if err := deleteIfExists(ctx, rr.Client, pvc, "model cache PVC"); err != nil {
		return err
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "kserve-localmodelnode-pv"},
	}
	if err := deleteIfExists(ctx, rr.Client, pv, "model cache PV"); err != nil {
		return err
	}

	lmng := &unstructured.Unstructured{}
	lmng.SetGroupVersionKind(gvk.LocalModelNodeGroup)
	lmng.SetName("workers")
	if err := deleteIfExists(ctx, rr.Client, lmng, "LocalModelNodeGroup"); err != nil {
		return err
	}

	nodeList := &corev1.NodeList{}
	if err := rr.Client.List(ctx, nodeList, client.MatchingLabels{modelCacheLabelKey: modelCacheLabelValue}); err != nil {
		return fmt.Errorf("failed to list model cache nodes: %w", err)
	}
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		original := node.DeepCopy()
		delete(node.Labels, modelCacheLabelKey)
		if err := rr.Client.Patch(ctx, node, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("failed to unlabel node %q: %w", node.Name, err)
		}
		logger.Info("Removed model cache label from node", "node", node.Name)
	}

	return nil
}

func updateNamespacePSA(ctx context.Context, cli client.Client, desiredLevel string) error {
	logger := logf.FromContext(ctx)

	ns := &corev1.Namespace{}
	if err := cli.Get(ctx, client.ObjectKey{Name: cluster.GetApplicationNamespace()}, ns); err != nil {
		return fmt.Errorf("failed to get application namespace: %w", err)
	}

	original := ns.DeepCopy()

	current := ns.Labels[labels.SecurityEnforce]
	currentAnnotation := resources.GetAnnotation(ns, annotations.PSAElevatedBy)
	needsUpdate := false

	if current != desiredLevel {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels[labels.SecurityEnforce] = desiredLevel
		needsUpdate = true
	}

	if desiredLevel == "privileged" && currentAnnotation != "kserve-modelcache" {
		resources.SetAnnotation(ns, annotations.PSAElevatedBy, "kserve-modelcache")
		needsUpdate = true
	} else if desiredLevel != "privileged" && currentAnnotation != "" {
		resources.RemoveAnnotation(ns, annotations.PSAElevatedBy)
		needsUpdate = true
	}

	if !needsUpdate {
		return nil
	}

	if err := cli.Patch(ctx, ns, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("failed to update namespace PSA label: %w", err)
	}

	logger.Info("Updated namespace PSA enforcement level",
		"namespace", ns.Name,
		"from", current,
		"to", desiredLevel)

	return nil
}

func forceReconcileKserveAgentImage(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	logger := logf.FromContext(ctx)

	expectedImage := os.Getenv(imageParamMap["kserve-agent"])
	if expectedImage == "" {
		return nil
	}

	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{
		Namespace: cluster.GetApplicationNamespace(),
		Name:      kserveConfigMapName,
	}

	if err := rr.Client.Get(ctx, key, cm); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get ConfigMap %s: %w", kserveConfigMapName, err)
	}

	openshiftConfigRaw, ok := cm.Data[OpenshiftConfigKeyName]
	if !ok {
		return nil
	}

	var openshiftConfig map[string]any
	if err := json.Unmarshal([]byte(openshiftConfigRaw), &openshiftConfig); err != nil {
		return fmt.Errorf("error parsing %s in ConfigMap %s: %w", OpenshiftConfigKeyName, kserveConfigMapName, err)
	}

	currentImage, _ := openshiftConfig["modelcachePermissionFixImage"].(string)
	if currentImage == expectedImage {
		return nil
	}

	openshiftConfig["modelcachePermissionFixImage"] = expectedImage
	updated, err := json.MarshalIndent(openshiftConfig, "", " ")
	if err != nil {
		return fmt.Errorf("error marshaling %s: %w", OpenshiftConfigKeyName, err)
	}
	cm.Data[OpenshiftConfigKeyName] = string(updated)

	if err := rr.Client.Update(ctx, cm); err != nil {
		return fmt.Errorf("failed to update ConfigMap %s: %w", kserveConfigMapName, err)
	}

	logger.Info("Force-reconciled modelcachePermissionFixImage in inferenceservice-config",
		"image", expectedImage)

	return nil
}

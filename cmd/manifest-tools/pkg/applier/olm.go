package applier

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var subscriptionGVR = schema.GroupVersionResource{
	Group:    "operators.coreos.com",
	Version:  "v1alpha1",
	Resource: "subscriptions",
}

type Options struct {
	ConfigFile      string
	Platform        string
	Namespace       string
	OperatorPackage string
}

type envVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func ApplyOLM(opts Options) error {
	envVars, err := loadOverridesFromConfig(opts.ConfigFile, opts.Platform)
	if err != nil {
		return err
	}

	if len(envVars) == 0 {
		slog.Info("No overrides to apply")
		return nil
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return fmt.Errorf("loading kubeconfig: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("creating dynamic client: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("creating clientset: %w", err)
	}

	ctx := context.Background()

	subName, err := findSubscription(ctx, dynClient, opts.Namespace, opts.OperatorPackage)
	if err != nil {
		if isOLMAPIUnavailable(err) {
			slog.Info("OLM Subscriptions API not available, skipping OLM image overrides",
				slog.String("namespace", opts.Namespace))
			return nil
		}
		return fmt.Errorf("listing subscriptions in namespace %s: %w", opts.Namespace, err)
	}

	if subName == "" {
		return fmt.Errorf("no Subscription for package %q found in namespace %s", opts.OperatorPackage, opts.Namespace)
	}

	return applyToSubscription(ctx, dynClient, clientset, opts.Namespace, subName, opts.OperatorPackage, envVars, 120*time.Second)
}

func isOLMAPIUnavailable(err error) bool {
	return meta.IsNoMatchError(err) || apierrors.IsNotFound(err)
}

func applyToSubscription(ctx context.Context, dynClient dynamic.Interface, clientset kubernetes.Interface,
	namespace, subName, operatorPackage string, envVars []envVar, rolloutTimeout time.Duration) error {

	slog.Info("Found Subscription", slog.String("name", subName), slog.String("namespace", namespace))

	sub, err := dynClient.Resource(subscriptionGVR).Namespace(namespace).Get(ctx, subName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting subscription: %w", err)
	}

	existingEnv, found, err := unstructured.NestedSlice(sub.Object, "spec", "config", "env")
	if err != nil {
		return fmt.Errorf("reading subscription env: %w", err)
	}
	if !found {
		existingEnv = []any{}
	}
	envMap := make(map[string]int, len(existingEnv))
	var merged []envVar
	for i, raw := range existingEnv {
		if m, ok := raw.(map[string]any); ok {
			name, _ := m["name"].(string)
			value, _ := m["value"].(string)
			merged = append(merged, envVar{Name: name, Value: value})
			envMap[name] = i
		}
	}
	for _, ev := range envVars {
		if idx, ok := envMap[ev.Name]; ok {
			merged[idx] = ev
		} else {
			merged = append(merged, ev)
		}
	}

	patch := map[string]any{
		"spec": map[string]any{
			"config": map[string]any{
				"env": merged,
			},
		},
	}

	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}

	slog.Info("Patching Subscription", slog.String("name", subName), slog.Int("overrides", len(envVars)), slog.Int("totalEnvVars", len(merged)))
	_, err = dynClient.Resource(subscriptionGVR).Namespace(namespace).Patch(
		ctx, subName, types.MergePatchType, patchJSON, metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patching subscription: %w", err)
	}

	deployName, err := findDeployment(ctx, clientset, namespace, operatorPackage)
	if err != nil {
		return fmt.Errorf("finding operator deployment in namespace %s: %w", namespace, err)
	}
	if deployName == "" {
		return fmt.Errorf("no operator deployment found in namespace %s after patching Subscription", namespace)
	}

	slog.Info("Waiting for Deployment rollout...")
	if err := waitForRollout(ctx, clientset, namespace, deployName, rolloutTimeout); err != nil {
		return fmt.Errorf("waiting for deployment %s rollout in namespace %s: %w", deployName, namespace, err)
	}

	slog.Info("Image overrides applied via Subscription")
	return nil
}

func findSubscription(ctx context.Context, client dynamic.Interface, namespace, packageName string) (string, error) {
	list, err := client.Resource(subscriptionGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	var matches []string
	for _, item := range list.Items {
		specName, _, _ := unstructured.NestedString(item.Object, "spec", "name")
		if specName == packageName {
			matches = append(matches, item.GetName())
		}
	}

	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple Subscriptions for package %q found in namespace %s: %v", packageName, namespace, matches)
	}
}

func findDeployment(ctx context.Context, client kubernetes.Interface, namespace, operatorPackage string) (string, error) {
	selectors := []string{"control-plane=controller-manager"}
	if operatorPackage != "" {
		selectors = append(selectors, fmt.Sprintf("name=%s", operatorPackage))
	}

	for _, selector := range selectors {
		deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			return "", fmt.Errorf("listing deployments with selector %q: %w", selector, err)
		}
		if len(deployments.Items) > 0 {
			return deployments.Items[0].Name, nil
		}
	}

	return "", nil
}

func waitForRollout(ctx context.Context, client kubernetes.Interface, namespace, deployName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		deploy, err := client.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		var replicas int32 = 1
		if deploy.Spec.Replicas != nil {
			replicas = *deploy.Spec.Replicas
		}

		if deploy.Status.UpdatedReplicas == replicas &&
			deploy.Status.ReadyReplicas == replicas &&
			deploy.Status.AvailableReplicas == replicas &&
			deploy.Status.ObservedGeneration >= deploy.Generation {
			slog.Info("Deployment rolled out successfully", slog.String("deployment", deployName), slog.Int("ready", int(deploy.Status.ReadyReplicas)), slog.Int("replicas", int(replicas)))
			return nil
		}

		slog.Info("Waiting for rollout",
			slog.String("deployment", deployName),
			slog.Int("ready", int(deploy.Status.ReadyReplicas)),
			slog.Int("replicas", int(replicas)),
			slog.Int("updated", int(deploy.Status.UpdatedReplicas)))
		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timeout waiting for deployment %s rollout", deployName)
}

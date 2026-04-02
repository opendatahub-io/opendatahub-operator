package clusterhealth

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var dependentOperatorNamespaces = []struct{ name, namespace string }{
	{"opentelemetry", "openshift-opentelemetry-operator"},
	{"tempo", "openshift-tempo-operator"},
	{"cluster-observability", "openshift-cluster-observability-operator"},
	{"kueue", "openshift-kueue-operator"},
	{"jobset", "openshift-jobset-operator"},
	{"leader-worker-set", "openshift-lws-operator"},
	{"cert-manager", "cert-manager-operator"},
	{"kuadrant", "kuadrant-system"},
}

func runOperatorSection(ctx context.Context, c client.Client, op OperatorConfig) SectionResult[OperatorSection] {
	var out SectionResult[OperatorSection]
	var errs []string

	if op.Namespace == "" || op.Name == "" {
		out.Error = "operator config missing namespace or name"
		return out
	}

	deploy := &appsv1.Deployment{}
	err := c.Get(ctx, types.NamespacedName{Namespace: op.Namespace, Name: op.Name}, deploy)
	if err != nil {
		out.Error = fmt.Sprintf("operator deployment not found: %v", err)
		return out
	}

	depInfo := deploymentToInfo(deploy)
	out.Data.Deployment = &depInfo
	out.Data.Data = &OperatorSectionData{Deployment: deploy}

	if len(deploy.Spec.Selector.MatchLabels) == 0 {
		out.Data.Pods = []PodInfo{}
		out.Error = "operator deployment has no selector"
		return out
	}
	pods, rawPods, listErr := listPodsInNamespace(ctx, c, op.Namespace, deploy.Spec.Selector.MatchLabels)
	if listErr != nil {
		out.Error = fmt.Sprintf("list operator pods: %v", listErr)
		out.Data.Pods = []PodInfo{}
		return out
	}
	out.Data.Pods = pods
	if out.Data.Data != nil {
		out.Data.Data.Pods = rawPods
	}

	desired := desiredReplicas(deploy)
	if desired == 0 {
		errs = append(errs, fmt.Sprintf("operator deployment %s: scaled to 0 replicas", op.Name))
	} else if deploy.Status.ReadyReplicas < desired {
		errs = append(errs, fmt.Sprintf("operator deployment %s: %d/%d ready", op.Name, deploy.Status.ReadyReplicas, desired))
	}
	for _, p := range out.Data.Pods {
		if p.Phase != string(corev1.PodRunning) {
			errs = append(errs, fmt.Sprintf("operator pod %s: %s", p.Name, p.Phase))
		}
	}

	// Run dependent-operator checks in parallel; collect in same order as dependentOperatorNamespaces.
	out.Data.DependentOperators = make([]DependentOperatorResult, 0, len(dependentOperatorNamespaces))
	depResults := make([]DependentOperatorResult, len(dependentOperatorNamespaces))
	g, gctx := errgroup.WithContext(ctx)
	for i := range dependentOperatorNamespaces {
		d := dependentOperatorNamespaces[i]
		g.Go(func() error {
			depResults[i] = runDependentOperatorCheck(gctx, c, d.name, d.namespace)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		out.Error = strings.Join(append(errs, fmt.Sprintf("dependent checks: %v", err)), "; ")
		return out
	}
	for _, dep := range depResults {
		out.Data.DependentOperators = append(out.Data.DependentOperators, dep)
		if dep.Error != "" {
			errs = append(errs, fmt.Sprintf("dependent %s: %s", dep.Name, dep.Error))
		}
	}

	if len(errs) > 0 {
		out.Error = strings.Join(errs, "; ")
	}
	return out
}

// findOperatorDeploymentInNamespace lists deployments in the namespace and returns the one to use for health.
// Prefers a deployment whose name contains "operator"; otherwise returns the first.
func findOperatorDeploymentInNamespace(ctx context.Context, c client.Client, namespace string) (*appsv1.Deployment, error) {
	list := &appsv1.DeploymentList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	if len(list.Items) == 0 {
		return nil, nil
	}
	// Sort by name for deterministic choice.
	items := list.Items
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	// Prefer a deployment whose name contains "operator" (main operator deploy).
	for i := range items {
		if strings.Contains(items[i].Name, "operator") {
			return &items[i], nil
		}
	}
	// Otherwise use the first
	return &items[0], nil
}

func runDependentOperatorCheck(ctx context.Context, c client.Client, name, namespace string) DependentOperatorResult {
	out := DependentOperatorResult{Name: name}
	deploy, err := findOperatorDeploymentInNamespace(ctx, c, namespace)
	if err != nil {
		if k8serr.IsForbidden(err) {
			out.Error = "list deployments: permission denied"
		} else {
			out.Error = fmt.Sprintf("list deployments: %v", err)
		}
		return out
	}
	if deploy == nil {
		// No deployments in namespace — not installed; record it so callers can see which dependents are missing.
		out.Installed = false
		return out
	}
	out.Installed = true
	depInfo := deploymentToInfo(deploy)
	out.Deployment = &depInfo
	// we don't expect this to ever be empty for an operator but check it just in case
	if len(deploy.Spec.Selector.MatchLabels) == 0 {
		return out
	}
	pods, _, listErr := listPodsInNamespace(ctx, c, namespace, deploy.Spec.Selector.MatchLabels)
	if listErr != nil {
		out.Error = fmt.Sprintf("list pods: %v", listErr)
		return out
	}
	out.Pods = pods
	desired := desiredReplicas(deploy)
	if desired > 0 && deploy.Status.ReadyReplicas < desired {
		out.Error = fmt.Sprintf("deployment %d/%d ready", deploy.Status.ReadyReplicas, desired)
	}
	for _, p := range out.Pods {
		if p.Phase != string(corev1.PodRunning) {
			if out.Error != "" {
				out.Error += "; "
			}
			out.Error += fmt.Sprintf("pod %s: %s", p.Name, p.Phase)
		}
	}
	return out
}

package clusterhealth

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func runDeploymentsSection(ctx context.Context, c client.Client, ns NamespaceConfig) SectionResult[DeploymentsSection] {
	var out SectionResult[DeploymentsSection]
	out.Data.ByNamespace = make(map[string][]DeploymentInfo)

	namespaces := ns.List()
	if len(namespaces) == 0 {
		return out
	}

	var errs []string
	for _, namespace := range namespaces {
		infos, raw, err := listDeploymentsInNamespace(ctx, c, namespace)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", namespace, err))
			continue
		}
		out.Data.ByNamespace[namespace] = infos
		out.Data.Data = append(out.Data.Data, raw...)
	}

	var notReady []string
	for nsName, infos := range out.Data.ByNamespace {
		for _, info := range infos {
			if info.Replicas > 0 && info.Ready != info.Replicas {
				notReady = append(notReady, fmt.Sprintf("%s/%s (%d/%d)", nsName, info.Name, info.Ready, info.Replicas))
			}
		}
	}
	if len(notReady) > 0 {
		errs = append(errs, fmt.Sprintf("deployments not ready: %s", strings.Join(notReady, ", ")))
	}
	if len(errs) > 0 {
		out.Error = strings.Join(errs, "; ")
	}
	return out
}

func listDeploymentsInNamespace(ctx context.Context, c client.Client, namespace string) ([]DeploymentInfo, []appsv1.Deployment, error) {
	list := &appsv1.DeploymentList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, nil, err
	}
	infos := make([]DeploymentInfo, 0, len(list.Items))
	for i := range list.Items {
		d := &list.Items[i]
		infos = append(infos, deploymentToInfo(d))
	}
	return infos, list.Items, nil
}

// desiredReplicas returns the deployment's desired replica count from spec (default 1 if nil).
func desiredReplicas(d *appsv1.Deployment) int32 {
	if d.Spec.Replicas == nil {
		return 1
	}
	return *d.Spec.Replicas
}

func deploymentToInfo(d *appsv1.Deployment) DeploymentInfo {
	info := DeploymentInfo{
		Namespace: d.Namespace,
		Name:      d.Name,
		Ready:     d.Status.ReadyReplicas,
		Replicas:  desiredReplicas(d),
	}
	for _, c := range d.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			info.Conditions = append(info.Conditions, ConditionSummary{
				Type:    string(c.Type),
				Status:  string(c.Status),
				Message: c.Message,
			})
		}
	}
	return info
}

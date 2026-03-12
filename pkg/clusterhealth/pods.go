package clusterhealth

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const restartWarningThreshold int32 = 3

func runPodsSection(ctx context.Context, c client.Client, ns NamespaceConfig, logCfg logConfig) SectionResult[PodsSection] {
	var out SectionResult[PodsSection]
	out.Data.ByNamespace = make(map[string][]PodInfo)
	namespaces := ns.List()
	if len(namespaces) == 0 {
		return out
	}

	var errs []string
	for _, namespace := range namespaces {
		infos, raw, err := listPodsInNamespace(ctx, c, namespace, nil)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", namespace, err))
			continue
		}
		out.Data.ByNamespace[namespace] = infos
		out.Data.Data = append(out.Data.Data, raw...)
	}

	// Capture logs for problematic containers across all namespaces.
	for ns := range out.Data.ByNamespace {
		pods := out.Data.ByNamespace[ns]
		captureLogsForPods(ctx, logCfg.clientset, logCfg.tailLines, pods)
		out.Data.ByNamespace[ns] = pods
	}

	for _, infos := range out.Data.ByNamespace {
		for _, info := range infos {
			if reason := podUnhealthyReason(&info); reason != "" {
				errs = append(errs, fmt.Sprintf("%s/%s: %s", info.Namespace, info.Name, reason))
			}
		}
	}
	if len(errs) > 0 {
		out.Error = strings.Join(errs, "; ")
	}
	return out
}

func listPodsInNamespace(ctx context.Context, c client.Client, namespace string, selector map[string]string) ([]PodInfo, []corev1.Pod, error) {
	list := &corev1.PodList{}
	opts := []client.ListOption{client.InNamespace(namespace)}
	if len(selector) > 0 {
		opts = append(opts, client.MatchingLabels(selector))
	}
	if err := c.List(ctx, list, opts...); err != nil {
		return nil, nil, err
	}
	infos := make([]PodInfo, 0, len(list.Items))
	for i := range list.Items {
		infos = append(infos, podToInfo(&list.Items[i]))
	}
	return infos, list.Items, nil
}

func podToInfo(pod *corev1.Pod) PodInfo {
	info := PodInfo{
		Namespace: pod.Namespace,
		Name:      pod.Name,
		Phase:     string(pod.Status.Phase),
	}
	for i := range pod.Status.ContainerStatuses {
		info.Containers = append(info.Containers, containerStatusToInfo(&pod.Status.ContainerStatuses[i]))
	}
	for i := range pod.Status.InitContainerStatuses {
		info.Containers = append(info.Containers, containerStatusToInfo(&pod.Status.InitContainerStatuses[i]))
	}
	return info
}

func containerStatusToInfo(cs *corev1.ContainerStatus) ContainerInfo {
	info := ContainerInfo{
		Name:         cs.Name,
		Ready:        cs.Ready,
		RestartCount: cs.RestartCount,
	}
	if cs.State.Waiting != nil {
		info.Waiting = strings.TrimSpace(cs.State.Waiting.Reason + " " + cs.State.Waiting.Message)
	}
	if cs.State.Terminated != nil {
		info.Terminated = fmt.Sprintf("%s (exit %d)", cs.State.Terminated.Reason, cs.State.Terminated.ExitCode)
		if cs.State.Terminated.Message != "" {
			info.Terminated += ": " + cs.State.Terminated.Message
		}
	}
	return info
}

func podUnhealthyReason(info *PodInfo) string {
	if info.Phase != string(corev1.PodRunning) && info.Phase != string(corev1.PodSucceeded) {
		return "phase=" + info.Phase
	}
	// Succeeded pods sometimes have terminated containers; only check container state when Running.
	if info.Phase != string(corev1.PodRunning) {
		return ""
	}
	for _, c := range info.Containers {
		if !c.Ready {
			return "container " + c.Name + " not ready"
		}
		// Restarts alone don't make a Running+Ready container unhealthy — the restart
		// count is cumulative and never resets. High counts are flagged in long-format
		// details but don't cause a section failure.
		if c.Waiting != "" {
			return "container " + c.Name + " waiting: " + c.Waiting
		}
		if c.Terminated != "" {
			return "container " + c.Name + " " + c.Terminated
		}
	}
	return ""
}

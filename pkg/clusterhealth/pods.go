package clusterhealth

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func runPodsSection(ctx context.Context, c client.Client, ns NamespaceConfig) SectionResult[PodsSection] {
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
		NodeName:  pod.Spec.NodeName,
		CreatedAt: pod.CreationTimestamp.Time,
	}

	// Map to look up container specs by name to extract requests/limits
	containerSpecs := make(map[string]*corev1.Container)
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		containerSpecs[c.Name] = c
	}
	for i := range pod.Spec.InitContainers {
		c := &pod.Spec.InitContainers[i]
		containerSpecs[c.Name] = c
	}

	for i := range pod.Status.ContainerStatuses {
		cs := &pod.Status.ContainerStatuses[i]
		cinfo := containerStatusToInfo(cs)
		if spec, ok := containerSpecs[cs.Name]; ok {
			enrichWithResources(&cinfo, spec)
		}
		info.Containers = append(info.Containers, cinfo)
	}
	for i := range pod.Status.InitContainerStatuses {
		cs := &pod.Status.InitContainerStatuses[i]
		cinfo := containerStatusToInfo(cs)
		if spec, ok := containerSpecs[cs.Name]; ok {
			enrichWithResources(&cinfo, spec)
		}
		info.Containers = append(info.Containers, cinfo)
	}
	return info
}

func enrichWithResources(info *ContainerInfo, spec *corev1.Container) {
	if reqCPU := spec.Resources.Requests[corev1.ResourceCPU]; !reqCPU.IsZero() {
		val := reqCPU.MilliValue()
		info.RequestsCPU = &val
	}
	if reqMem := spec.Resources.Requests[corev1.ResourceMemory]; !reqMem.IsZero() {
		val := reqMem.Value()
		info.RequestsMemory = &val
	}
	if limCPU := spec.Resources.Limits[corev1.ResourceCPU]; !limCPU.IsZero() {
		val := limCPU.MilliValue()
		info.LimitsCPU = &val
	}
	if limMem := spec.Resources.Limits[corev1.ResourceMemory]; !limMem.IsZero() {
		val := limMem.Value()
		info.LimitsMemory = &val
	}
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
		if c.RestartCount > 0 {
			return "container " + c.Name + " restarts=" + strconv.Itoa(int(c.RestartCount))
		}
		if c.Waiting != "" {
			return "container " + c.Name + " waiting: " + c.Waiting
		}
		if c.Terminated != "" {
			return "container " + c.Name + " " + c.Terminated
		}
	}
	return ""
}

package clusterhealth

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func runQuotasSection(ctx context.Context, c client.Client, ns NamespaceConfig) SectionResult[QuotasSection] {
	var out SectionResult[QuotasSection]
	out.Data.ByNamespace = make(map[string][]ResourceQuotaInfo)
	namespaces := ns.List()
	if len(namespaces) == 0 {
		return out
	}

	var errs []string
	for _, namespace := range namespaces {
		infos, raw, listErr := listResourceQuotasInNamespace(ctx, c, namespace)
		if listErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", namespace, listErr))
			continue
		}
		out.Data.ByNamespace[namespace] = infos
		out.Data.Data = append(out.Data.Data, raw...)
	}

	for _, infos := range out.Data.ByNamespace {
		for _, info := range infos {
			if len(info.Exceeded) > 0 {
				errs = append(errs, fmt.Sprintf("%s/%s exceeded: %s", info.Namespace, info.Name, strings.Join(info.Exceeded, ", ")))
			}
		}
	}
	if len(errs) > 0 {
		out.Error = strings.Join(errs, "; ")
	}
	return out
}

func listResourceQuotasInNamespace(ctx context.Context, c client.Client, namespace string) ([]ResourceQuotaInfo, []corev1.ResourceQuota, error) {
	list := &corev1.ResourceQuotaList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, nil, err
	}
	infos := make([]ResourceQuotaInfo, 0, len(list.Items))
	for i := range list.Items {
		infos = append(infos, resourceQuotaToInfo(&list.Items[i]))
	}
	return infos, list.Items, nil
}

func resourceQuotaToInfo(q *corev1.ResourceQuota) ResourceQuotaInfo {
	info := ResourceQuotaInfo{
		Namespace: q.Namespace,
		Name:      q.Name,
		Used:      make(map[string]string),
		Hard:      make(map[string]string),
	}
	for res, qty := range q.Status.Used {
		info.Used[string(res)] = qty.String()
	}
	for res, qty := range q.Status.Hard {
		info.Hard[string(res)] = qty.String()
	}
	for res, usedQty := range q.Status.Used {
		hardQty, hasHard := q.Status.Hard[res]
		if !hasHard {
			continue
		}
		if quantityExceedsOrEqual(usedQty, hardQty) {
			info.Exceeded = append(info.Exceeded, string(res))
		}
	}
	return info
}

func quantityExceedsOrEqual(used, hard resource.Quantity) bool {
	return used.Cmp(hard) >= 0
}

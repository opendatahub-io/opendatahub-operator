package clusterhealth

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const DefaultEventsWindow = 5 * time.Minute

func runEventsSection(ctx context.Context, c client.Client, ns NamespaceConfig, since time.Time) SectionResult[EventsSection] {
	var out SectionResult[EventsSection]
	cutoff := since.Add(-DefaultEventsWindow)
	namespaces := ns.List()
	if len(namespaces) == 0 {
		out.Data.Events = []EventInfo{}
		return out
	}

	var allEvents []EventInfo
	var allRaw []corev1.Event
	var errs []string
	for _, namespace := range namespaces {
		infos, raw, listErr := listRecentEventsInNamespace(ctx, c, namespace, cutoff)
		if listErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", namespace, listErr))
			continue
		}
		allEvents = append(allEvents, infos...)
		allRaw = append(allRaw, raw...)
	}

	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].LastTime.After(allEvents[j].LastTime)
	})
	out.Data.Events = allEvents
	out.Data.Data = allRaw
	if len(errs) > 0 {
		out.Error = strings.Join(errs, "; ")
	}
	return out
}

func listRecentEventsInNamespace(ctx context.Context, c client.Client, namespace string, cutoff time.Time) ([]EventInfo, []corev1.Event, error) {
	list := &corev1.EventList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, nil, err
	}
	infos := make([]EventInfo, 0, len(list.Items))
	raw := make([]corev1.Event, 0, len(list.Items))
	for i := range list.Items {
		e := &list.Items[i]
		if eventLastTime(e).Before(cutoff) {
			continue
		}
		infos = append(infos, eventToInfo(e))
		raw = append(raw, *e)
	}
	return infos, raw, nil
}

// eventLastTime returns the effective last time for an event (LastTimestamp with EventTime fallback).
func eventLastTime(e *corev1.Event) time.Time {
	t := e.LastTimestamp.Time
	if t.IsZero() && !e.EventTime.IsZero() {
		t = e.EventTime.Time
	}
	return t
}

func eventToInfo(e *corev1.Event) EventInfo {
	return EventInfo{
		Namespace: e.Namespace,
		Kind:      e.InvolvedObject.Kind,
		Name:      e.InvolvedObject.Name,
		Type:      e.Type,
		Reason:    e.Reason,
		Message:   e.Message,
		LastTime:  eventLastTime(e),
	}
}

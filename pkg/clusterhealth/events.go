package clusterhealth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const DefaultEventsWindow = 5 * time.Minute

func collectEvents(ctx context.Context, c client.Client, namespaces []string, cutoff time.Time) ([]EventInfo, []corev1.Event, map[string]error) {
	var allEvents []EventInfo
	var allRaw []corev1.Event
	var errs map[string]error
	for _, namespace := range namespaces {
		infos, raw, listErr := listRecentEventsInNamespace(ctx, c, namespace, cutoff)
		if listErr != nil {
			if errs == nil {
				errs = make(map[string]error)
			}
			errs[namespace] = listErr
			continue
		}
		allEvents = append(allEvents, infos...)
		allRaw = append(allRaw, raw...)
	}

	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].LastTime.After(allEvents[j].LastTime)
	})
	return allEvents, allRaw, errs
}

func runEventsSection(ctx context.Context, c client.Client, ns NamespaceConfig, since time.Time) SectionResult[EventsSection] {
	var out SectionResult[EventsSection]
	cutoff := since.Add(-DefaultEventsWindow)
	namespaces := ns.List()
	if len(namespaces) == 0 {
		out.Data.Events = []EventInfo{}
		return out
	}

	events, raw, errs := collectEvents(ctx, c, namespaces, cutoff)
	out.Data.Events = events
	out.Data.Data = raw
	if len(errs) > 0 {
		var msgs []string
		for ns, err := range errs {
			msgs = append(msgs, fmt.Sprintf("%s: %v", ns, err))
		}
		out.Error = strings.Join(msgs, "; ")
	}
	return out
}

func listRecentEventsInNamespace(ctx context.Context, c client.Client, namespace string, cutoff time.Time) ([]EventInfo, []corev1.Event, error) {
	var infos []EventInfo
	var raw []corev1.Event
	list := &corev1.EventList{}
	opts := []client.ListOption{client.InNamespace(namespace), client.Limit(500)}

	for {
		if err := c.List(ctx, list, opts...); err != nil {
			return nil, nil, err
		}
		for i := range list.Items {
			e := &list.Items[i]
			if eventLastTime(e).Before(cutoff) {
				continue
			}
			infos = append(infos, eventToInfo(e))
			raw = append(raw, *e)
		}
		if list.Continue == "" {
			break
		}
		opts = []client.ListOption{client.InNamespace(namespace), client.Limit(500), client.Continue(list.Continue)}
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

// RecentEventsConfig holds parameters for a standalone recent-events query.
type RecentEventsConfig struct {
	Client     client.Client
	Namespaces []string      // namespaces to scan (caller resolves these)
	Since      time.Duration // look-back window; zero = DefaultEventsWindow (5m)
	EventType  string        // "Warning", "Normal", or "" for all
}

// RunRecentEvents returns recent events across namespaces, filtered by type
// and sorted most-recent-first.
func RunRecentEvents(ctx context.Context, cfg RecentEventsConfig) ([]EventInfo, error) {
	if cfg.Client == nil {
		return nil, errors.New("clusterhealth: client is required")
	}
	if len(cfg.Namespaces) == 0 {
		return []EventInfo{}, nil
	}
	if cfg.Since < 0 {
		return nil, errors.New("clusterhealth: since must be non-negative")
	}
	if cfg.Since == 0 {
		cfg.Since = DefaultEventsWindow
	}
	cutoff := time.Now().Add(-cfg.Since)

	all, _, errs := collectEvents(ctx, cfg.Client, cfg.Namespaces, cutoff)

	if cfg.EventType != "" {
		filtered := all[:0]
		for _, e := range all {
			if strings.EqualFold(e.Type, cfg.EventType) {
				filtered = append(filtered, e)
			}
		}
		all = filtered
	}

	if len(errs) > 0 {
		var msgs []string
		for ns, err := range errs {
			switch {
			case apierrors.IsNotFound(err):
				msgs = append(msgs, fmt.Sprintf("%s: namespace not found", ns))
			case apierrors.IsForbidden(err):
				msgs = append(msgs, fmt.Sprintf("%s: forbidden (missing RBAC permissions)", ns))
			default:
				msgs = append(msgs, fmt.Sprintf("%s: %v", ns, err))
			}
		}
		return all, errors.New(strings.Join(msgs, "; "))
	}
	return all, nil
}

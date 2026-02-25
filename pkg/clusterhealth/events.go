package clusterhealth

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// runEventsSection collects recent (e.g. warning) events from configured namespaces.
func runEventsSection(ctx context.Context, c client.Client, ns NamespaceConfig, since time.Time) SectionResult[EventsSection] {
	var out SectionResult[EventsSection]
	_ = ctx
	_ = c
	_ = ns
	_ = since
	// Stub: return empty data
	out.Data.Events = nil
	return out
}

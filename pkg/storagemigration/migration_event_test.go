/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

//nolint:testpackage // Events are private to the migration implementation.
package storagemigration

import (
	"errors"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"

	. "github.com/onsi/gomega"
)

func TestMigrationEvent(t *testing.T) {
	failure := errors.New("migration failed")

	for _, test := range []struct {
		name      string
		event     migrationEvent
		eventType string
		message   string
	}{
		{
			name: "formatted message",
			event: migrationEvent{
				reason:  validatedReason,
				action:  EventActionValidate,
				message: fmt.Sprintf("Migrated %d%% of %s", 100, "widgets"),
			},
			eventType: corev1.EventTypeNormal,
			message:   "Migrated 100% of widgets",
		},
		{
			name: "error",
			event: migrationEvent{
				reason: failedReason,
				action: runAction,
				err:    failure,
			},
			eventType: corev1.EventTypeWarning,
			message:   "migration failed",
		},
		{
			name: "formatted error",
			event: migrationEvent{
				reason: failedReason,
				action: discoverAction,
				err:    fmt.Errorf("Migrating %s: %w", "widgets", failure),
			},
			eventType: corev1.EventTypeWarning,
			message:   "Migrating widgets: migration failed",
		},
		{
			name: "warning message",
			event: migrationEvent{
				reason:    discoveredReason,
				action:    discoverAction,
				message:   "Migration widgets is pending",
				eventType: corev1.EventTypeWarning,
			},
			eventType: corev1.EventTypeWarning,
			message:   "Migration widgets is pending",
		},
		{
			name: "type override",
			event: migrationEvent{
				reason:    failedReason,
				action:    cleanupAction,
				err:       failure,
				eventType: corev1.EventTypeNormal,
			},
			eventType: corev1.EventTypeNormal,
			message:   "migration failed",
		},
		{
			name: "message override preserves warning",
			event: migrationEvent{
				reason:  failedReason,
				action:  runAction,
				err:     failure,
				message: "Waiting for widgets failed",
			},
			eventType: corev1.EventTypeWarning,
			message:   "Waiting for widgets failed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			recorder := events.NewFakeRecorder(1)
			m := migration{
				crdName:  "widgets.example.test",
				recorder: recorder,
			}

			m.event(test.event)

			var recorded string
			g.Expect(recorder.Events).To(Receive(&recorded))
			g.Expect(recorded).To(Equal(test.eventType + " " + test.event.reason + " " + test.message))
			g.Expect(recorder.Events).To(BeEmpty())
		})
	}
}

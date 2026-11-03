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

package storagemigration

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	// EventReporter identifies events emitted by storage migration startup tasks.
	EventReporter = "storage-version-migration"

	// EventActionValidate identifies storage version validation events.
	EventActionValidate = "ValidateStorageVersion"

	// EventReasonNotNeeded indicates that a CRD already has current-only storage history.
	EventReasonNotNeeded = "StorageVersionMigrationNotNeeded"

	discoverAction = "DiscoverStorageVersion"
	runAction      = "MigrateStorageVersion"
	cleanupAction  = "CleanupStorageVersionMigration"

	validatedReason  = "StorageVersionMigrationValidated"
	discoveredReason = "StorageVersionMigrationDiscovered"
	succeededReason  = "StorageVersionMigrationSucceeded"
	cleanedUpReason  = "StorageVersionMigrationCleanedUp"
	failedReason     = "StorageVersionMigrationFailed"
)

type migrationEvent struct {
	reason    string
	action    string
	eventType string
	err       error
	message   string
}

func (m *migration) event(event migrationEvent) {
	eventType := corev1.EventTypeNormal
	switch {
	case event.eventType != "":
		eventType = event.eventType
	case event.err != nil:
		eventType = corev1.EventTypeWarning
	}

	message := event.message
	if message == "" && event.err != nil {
		message = event.err.Error()
	}

	regarding := &corev1.ObjectReference{
		APIVersion: gvk.CustomResourceDefinition.GroupVersion().String(),
		Kind:       gvk.CustomResourceDefinition.Kind,
		Name:       m.crdName,
	}
	if m.crd != nil {
		regarding.UID = m.crd.UID
	}

	m.recorder.Eventf(regarding, nil, eventType, event.reason, event.action, "%s", message)
}

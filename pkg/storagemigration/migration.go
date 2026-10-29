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

// Package storagemigration migrates CRD storage through the cluster's migration controller.
package storagemigration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/events"
)

type migration struct {
	definitions apiextensions.CustomResourceDefinitionInterface
	requests    dynamic.Interface
	discovery   discovery.ServerResourcesInterfaceWithContext
	recorder    events.EventRecorder
	interval    time.Duration

	crdName string
	crd     *apiextensionsv1.CustomResourceDefinition
	storage string
	api     schema.GroupVersionResource
	request *unstructured.Unstructured
}

func (m *migration) execute(ctx context.Context) error {
	m.crd = nil
	m.request = nil

	switch err := ctx.Err(); {
	case errors.Is(err, context.Canceled):
		return err
	case err != nil:
		err = fmt.Errorf("%s for CRD %s: %w", EventActionValidate, m.crdName, err)
		m.event(migrationEvent{
			reason: failedReason,
			action: EventActionValidate,
			err:    err,
		})
		return err
	}

	needed, err := m.validate(ctx)
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case err != nil:
		err = fmt.Errorf("%s for CRD %s: %w", EventActionValidate, m.crdName, err)
		m.event(migrationEvent{
			reason: failedReason,
			action: EventActionValidate,
			err:    err,
		})
		return err
	case !needed:
		m.event(migrationEvent{
			reason:  EventReasonNotNeeded,
			action:  EventActionValidate,
			message: fmt.Sprintf("CRD %s does not need storage migration", m.crdName),
		})
		return nil
	}

	m.event(migrationEvent{
		reason:  validatedReason,
		action:  EventActionValidate,
		message: fmt.Sprintf("CRD %s requires storage migration to %s", m.crdName, m.storage),
	})

	switch err := m.discover(ctx); {
	case errors.Is(err, context.Canceled):
		return err
	case err != nil:
		err = fmt.Errorf("%s for CRD %s: %w", discoverAction, m.crdName, err)
		m.event(migrationEvent{
			reason: failedReason,
			action: discoverAction,
			err:    err,
		})
		return err
	}

	m.event(migrationEvent{
		reason:  discoveredReason,
		action:  discoverAction,
		message: fmt.Sprintf("Selected %s for CRD %s storage migration", m.api.GroupVersion(), m.crdName),
	})

	switch err := m.run(ctx); {
	case errors.Is(err, context.Canceled):
		return err
	case err != nil:
		err = fmt.Errorf("%s for CRD %s: %w", runAction, m.crdName, err)
		m.event(migrationEvent{
			reason: failedReason,
			action: runAction,
			err:    err,
		})
		return err
	}

	m.event(migrationEvent{
		reason: succeededReason,
		action: runAction,
		message: fmt.Sprintf("Request %s succeeded; CRD %s storage history now contains only %s",
			m.request.GetName(), m.crdName, m.storage),
	})

	switch err := m.cleanup(ctx); {
	case errors.Is(err, context.Canceled):
		return err
	case err != nil:
		err = fmt.Errorf("%s for CRD %s: %w", cleanupAction, m.crdName, err)
		m.event(migrationEvent{
			reason: failedReason,
			action: cleanupAction,
			err:    err,
		})
		return err
	}

	m.event(migrationEvent{
		reason:  cleanedUpReason,
		action:  cleanupAction,
		message: fmt.Sprintf("Successful migration request %s for CRD %s is no longer present", m.request.GetName(), m.crdName),
	})

	return nil
}

func (m *migration) validate(ctx context.Context) (bool, error) {
	crd, err := m.definitions.Get(ctx, m.crdName, metav1.GetOptions{})
	switch {
	case k8serr.IsNotFound(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("reading CRD: %w", err)
	}

	m.crd = crd
	storage, err := storageVersion(crd)
	switch {
	case err != nil:
		return false, err
	case slices.Equal(crd.Status.StoredVersions, []string{storage}):
		return false, nil
	}

	m.storage = storage

	return true, nil
}

func (m *migration) discover(ctx context.Context) error {
	api, err := discoverMigrationAPI(ctx, m.discovery)
	if err != nil {
		return err
	}

	m.api = api

	return nil
}

func (m *migration) run(ctx context.Context) error {
	expected, err := newRequest(m.crd, m.storage, m.api)
	if err != nil {
		return err
	}

	requests := m.requests.Resource(m.api)
	m.request, err = createOrGetMigrationRequest(ctx, requests, expected)
	if err != nil {
		return err
	}

	if err := validateRequest(m.request, expected); err != nil {
		return err
	}

	// A retained Failed request must be reviewed and deleted before retrying.
	if _, err := requestSucceeded(m.request); err != nil {
		return err
	}

	expected.SetUID(m.request.GetUID())
	err = wait.PollUntilContextCancel(ctx, m.interval, true, func(ctx context.Context) (bool, error) {
		current, err := requests.Get(ctx, expected.GetName(), metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("reading migration request: %w", err)
		}

		if err := validateRequest(current, expected); err != nil {
			return false, err
		}

		succeeded, err := requestSucceeded(current)
		if err != nil {
			return false, err
		}

		return succeeded, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for %s request %s (retained for diagnosis): %w",
			m.api.GroupVersion(), expected.GetName(), err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := pruneStorageHistory(ctx, m.definitions, m.crd, m.storage); err != nil {
		return fmt.Errorf("pruning storage history (request %s retained): %w", m.request.GetName(), err)
	}

	return nil
}

func (m *migration) cleanup(ctx context.Context) error {
	err := m.requests.Resource(m.api).Delete(ctx, m.request.GetName(), metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID: new(m.request.GetUID()),
		},
	})

	switch {
	case err == nil:
		return nil
	case k8serr.IsNotFound(err):
		return nil
	default:
		return fmt.Errorf("storage history pruned, but deleting %s request %s failed: %w",
			m.api.GroupVersion(), m.request.GetName(), err)
	}
}

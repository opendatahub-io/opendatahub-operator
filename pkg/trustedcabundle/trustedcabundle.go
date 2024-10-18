// Package trustedcabundle provides utility functions to create and check trusted CA bundle configmap from DSCI CRD
package trustedcabundle

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	annotation "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	CAConfigMapName = "odh-trusted-ca-bundle"
	CADataFieldName = "odh-ca-bundle.crt"
)

func ShouldInjectTrustedBundle(ns *corev1.Namespace) bool {
	isActive := ns.Status.Phase == corev1.NamespaceActive
	return isActive && cluster.IsNotReservedNamespace(ns) && !HasCABundleAnnotationDisabled(ns)
}

// HasCABundleAnnotationDisabled checks if a namespace has the annotation "security.opendatahub.io/inject-trusted-ca-bundle" set to "false".
//
// It returns false if the annotation is set to "true", not set, or cannot be parsed as a boolean.
func HasCABundleAnnotationDisabled(ns client.Object) bool {
	if value, found := ns.GetAnnotations()[annotation.InjectionOfCABundleAnnotatoion]; found {
		shouldInject, err := strconv.ParseBool(value)
		return err == nil && !shouldInject
	}
	return false
}

// CreateOdhTrustedCABundleConfigMap creates a configMap 'odh-trusted-ca-bundle' in given namespace with labels and data
// or update existing odh-trusted-ca-bundle configmap if already exists with new content of .data.odh-ca-bundle.crt
// this is certificates for the cluster trusted CA Cert Bundle.
func CreateOdhTrustedCABundleConfigMap(ctx context.Context, cli client.Client, namespace string, customCAData string) error {
	// Expected configmap for the given namespace
	desiredConfigMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      CAConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				labels.K8SCommon.PartOf: "opendatahub-operator",
				// Label 'config.openshift.io/inject-trusted-cabundle' required for the Cluster Network Operator(CNO)
				// to inject the cluster trusted CA bundle into .data["ca-bundle.crt"]
				labels.InjectTrustCA: "true",
			},
		},
		// Add the DSCInitialzation specified TrustedCABundle.CustomCABundle to CM's data.odh-ca-bundle.crt field
		// Additionally, the CNO operator will automatically create and maintain ca-bundle.crt
		//  if label 'config.openshift.io/inject-trusted-cabundle' is true
		Data: map[string]string{CADataFieldName: customCAData},
	}

	// Create Configmap if doesn't exist
	foundConfigMap := &corev1.ConfigMap{}
	if err := cli.Get(ctx, client.ObjectKey{
		Name:      CAConfigMapName,
		Namespace: namespace,
	}, foundConfigMap); err != nil {
		if k8serr.IsNotFound(err) {
			err = cli.Create(ctx, desiredConfigMap)
			if err != nil && !k8serr.IsAlreadyExists(err) {
				return err
			}
			return nil
		}
		return err
	}

	if foundConfigMap.Data[CADataFieldName] != customCAData {
		foundConfigMap.Data[CADataFieldName] = customCAData
		return cli.Update(ctx, foundConfigMap)
	}

	return nil
}

func DeleteOdhTrustedCABundleConfigMap(ctx context.Context, cli client.Client, namespace string) error {
	// Delete Configmap if exists
	foundConfigMap := &corev1.ConfigMap{}
	if err := cli.Get(ctx, client.ObjectKey{
		Name:      CAConfigMapName,
		Namespace: namespace,
	}, foundConfigMap); err != nil {
		return client.IgnoreNotFound(err)
	}
	return cli.Delete(ctx, foundConfigMap)
}

// IsTrustedCABundleUpdated check if data in CM "odh-trusted-ca-bundle" from applciation namespace matches DSCI's TrustedCABundle.CustomCABundle
// return false when these two are matching => skip update
// return true when not match => need upate.
func IsTrustedCABundleUpdated(ctx context.Context, cli client.Client, dscInit *dsciv1.DSCInitialization) (bool, error) {
	userNamespace := &corev1.Namespace{}
	if err := cli.Get(ctx, client.ObjectKey{Name: dscInit.Spec.ApplicationsNamespace}, userNamespace); err != nil {
		if k8serr.IsNotFound(err) {
			// if namespace is not found, return true. This is to ensure we reconcile, and check for other namespaces.
			return true, nil
		}
		return false, err
	}

	if !ShouldInjectTrustedBundle(userNamespace) {
		return false, nil
	}

	foundConfigMap := &corev1.ConfigMap{}
	if err := cli.Get(ctx, client.ObjectKey{
		Name:      CAConfigMapName,
		Namespace: dscInit.Spec.ApplicationsNamespace,
	}, foundConfigMap); err != nil {
		return false, client.IgnoreNotFound(err)
	}

	return foundConfigMap.Data[CADataFieldName] != dscInit.Spec.TrustedCABundle.CustomCABundle, nil
}

func ConfigureTrustedCABundle(ctx context.Context, cli client.Client, log logr.Logger, dscInit *dsciv1.DSCInitialization, managementStateChanged bool) error {
	if dscInit.Spec.TrustedCABundle == nil {
		log.Info("Trusted CA Bundle is not configed in DSCI, same as default to `Removed` state. Reconciling to delete all " + CAConfigMapName)
		if err := RemoveCABundleCMInAllNamespaces(ctx, cli); err != nil {
			return fmt.Errorf("error deleting configmap %s from all valid namespaces %w", CAConfigMapName, err)
		}
		return nil
	}

	switch dscInit.Spec.TrustedCABundle.ManagementState {
	case operatorv1.Managed:
		log.Info("Trusted CA Bundle injection is set to `Managed` state. Reconciling to add/update " + CAConfigMapName)
		istrustedCABundleUpdated, err := IsTrustedCABundleUpdated(ctx, cli, dscInit)
		if err != nil {
			return err
		}

		if istrustedCABundleUpdated || managementStateChanged {
			if err := AddCABundleCMInAllNamespaces(ctx, cli, log, dscInit); err != nil {
				return fmt.Errorf("failed adding configmap %s to all namespaces: %w", CAConfigMapName, err)
			}
		}
	case operatorv1.Removed:
		log.Info("Trusted CA Bundle injection is set to `Removed` state. Reconciling to delete all " + CAConfigMapName)
		if err := RemoveCABundleCMInAllNamespaces(ctx, cli); err != nil {
			return fmt.Errorf("error deleting configmap %s from all namespaces %w", CAConfigMapName, err)
		}
	case operatorv1.Unmanaged:
		log.Info("Trusted CA Bundle injection is set to `Unmanaged` state. " + CAConfigMapName + " configmaps are no longer managed by operator")
	}

	return nil
}

// AddCABundleCMInAllNamespaces create or update trustCABundle configmap in namespaces.
func AddCABundleCMInAllNamespaces(ctx context.Context, cli client.Client, log logr.Logger, dscInit *dsciv1.DSCInitialization) error {
	var multiErr *multierror.Error
	processErr := cluster.ExecuteOnAllNamespaces(ctx, cli, func(ns *corev1.Namespace) error {
		if ShouldInjectTrustedBundle(ns) { // only work on namespace that meet requirements and status active
			pollErr := wait.PollUntilContextTimeout(ctx, time.Second*1, time.Second*10, false, func(ctx context.Context) (bool, error) {
				if cmErr := CreateOdhTrustedCABundleConfigMap(ctx, cli, ns.Name, dscInit.Spec.TrustedCABundle.CustomCABundle); cmErr != nil {
					// Logging the error for debugging
					log.Info("error creating cert configmap in namespace", "namespace", ns.Name, "error", cmErr)
					return false, nil
				}
				return true, nil
			})
			multiErr = multierror.Append(multiErr, pollErr)
		}
		return nil // Always return nil to continue processing
	})
	if processErr != nil {
		return processErr
	}
	return multierror.Append(multiErr, processErr).ErrorOrNil()
}

// RemoveCABundleCMInAllNamespaces delete trustCABundle configmap from namespaces.
func RemoveCABundleCMInAllNamespaces(ctx context.Context, cli client.Client) error {
	var multiErr *multierror.Error
	processErr := cluster.ExecuteOnAllNamespaces(ctx, cli, func(ns *corev1.Namespace) error {
		if !ShouldInjectTrustedBundle(ns) { // skip deletion if namespace does not match critieria
			return nil
		}
		multiErr = multierror.Append(multiErr, DeleteOdhTrustedCABundleConfigMap(ctx, cli, ns.Name))
		return nil // Always return nil to continue processing
	})
	return multierror.Append(multiErr, processErr).ErrorOrNil()
}

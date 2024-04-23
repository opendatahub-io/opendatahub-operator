// Package trustedcabundle provides utility functions to create and check trusted CA bundle configmap from DSCI CRD
package trustedcabundle

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	annotation "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	CAConfigMapName = "odh-trusted-ca-bundle"
	CADataFieldName = "odh-ca-bundle.crt"
)

func ShouldInjectTrustedBundle(ns client.Object) bool {
	if !strings.HasPrefix(ns.GetName(), "openshift-") && !strings.HasPrefix(ns.GetName(), "kube-") &&
		ns.GetName() != "default" && ns.GetName() != "openshift" && !HasCABundleAnnotationDisabled(ns) {
		return true
	}
	return false
}

// return true if namespace has annotation "security.opendatahub.io/inject-trusted-ca-bundle: false"
// return false if annotation is "true" or not set.
func HasCABundleAnnotationDisabled(ns client.Object) bool {
	if value, found := ns.GetAnnotations()[annotation.InjectionOfCABundleAnnotatoion]; found {
		shouldInject, err := strconv.ParseBool(value)
		return err == nil && !shouldInject
	}
	return false
}

// createOdhTrustedCABundleConfigMap creates a configMap 'odh-trusted-ca-bundle' in given namespace with labels and data
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
		if apierrs.IsNotFound(err) {
			err = cli.Create(ctx, desiredConfigMap)
			if err != nil && !apierrs.IsAlreadyExists(err) {
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
func IsTrustedCABundleUpdated(ctx context.Context, cli client.Client, dscInit *dsci.DSCInitialization) (bool, error) {
	usernamespace := &corev1.Namespace{}
	if err := cli.Get(ctx, client.ObjectKey{Name: dscInit.Spec.ApplicationsNamespace}, usernamespace); err != nil {
		if apierrs.IsNotFound(err) {
			// if namespace is not found, return true. This is to ensure we reconcile, and check for other namespaces.
			return true, nil
		}
		return false, err
	}

	if !ShouldInjectTrustedBundle(usernamespace) {
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

func ConfigureTrustedCABundle(ctx context.Context, cli client.Client, log logr.Logger, dscInit *dsci.DSCInitialization, managementStateChanged bool) error {
	switch dscInit.Spec.TrustedCABundle.ManagementState {
	case operatorv1.Managed:
		log.Info("Trusted CA Bundle injection is set to `Managed` state. Reconciling to add/update " + CAConfigMapName)
		istrustedCABundleUpdated, err := IsTrustedCABundleUpdated(ctx, cli, dscInit)
		if err != nil {
			return err
		}

		if istrustedCABundleUpdated || managementStateChanged {
			if err := AddCABundleConfigMapInAllNamespaces(ctx, cli, dscInit); err != nil {
				log.Error(err, "error adding configmap to all namespaces", "name", CAConfigMapName)
				return err
			}
		}
	case operatorv1.Removed:
		log.Info("Trusted CA Bundle injection is set to `Removed` state. Reconciling to delete all " + CAConfigMapName)
		if err := RemoveCABundleConfigMapInAllNamespaces(ctx, cli).ErrorOrNil(); err != nil {
			log.Error(err, "error deleting configmap from all namespaces", "name", CAConfigMapName)
			return err
		}

	case operatorv1.Unmanaged:
		log.Info("Trusted CA Bundle injection is set to `Unmanaged` state. " + CAConfigMapName + " configmaps are no longer managed by operator")
	}

	return nil
}

// when DSCI TrustedCABundle.ManagementState is set to `Managed`.
func AddCABundleConfigMapInAllNamespaces(ctx context.Context, cli client.Client, dscInit *dsci.DSCInitialization) error {
	namespaceList := &corev1.NamespaceList{}
	if err := cli.List(ctx, namespaceList); err != nil {
		return err
	}

	for i := range namespaceList.Items {
		ns := &namespaceList.Items[i]
		// check namespace status if not Active, then skip
		if ns.Status.Phase != corev1.NamespaceActive {
			continue
		}

		if ShouldInjectTrustedBundle(ns) {
			if err := wait.PollUntilContextTimeout(ctx, time.Second*1, time.Second*10, false, func(ctx context.Context) (bool, error) {
				if cmErr := CreateOdhTrustedCABundleConfigMap(ctx, cli, ns.Name, dscInit.Spec.TrustedCABundle.CustomCABundle); cmErr != nil {
					// Logging the error for debugging
					fmt.Printf("error creating cert configmap in namespace %v: %v", ns.Name, cmErr)
					return false, nil
				}
				return true, nil
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// when DSCI TrustedCABundle.ManagementState is set to `Removed`.
func RemoveCABundleConfigMapInAllNamespaces(ctx context.Context, cli client.Client) *multierror.Error {
	var multiErr *multierror.Error

	namespaceList := &corev1.NamespaceList{}
	if err := cli.List(ctx, namespaceList); err != nil {
		return multierror.Append(multiErr, err)
	}

	for i := range namespaceList.Items {
		ns := &namespaceList.Items[i]
		// check namespace status if not Active, then skip
		if ns.Status.Phase != corev1.NamespaceActive {
			continue
		}
		if err := DeleteOdhTrustedCABundleConfigMap(ctx, cli, ns.Name); err != nil {
			multiErr = multierror.Append(multiErr, err)
		}
	}
	return multiErr
}

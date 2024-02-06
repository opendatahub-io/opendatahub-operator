package trustedcabundle

import (
	"context"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
)

const (
	injectionOfCABundleAnnotatoion = "security.opendatahub.io/inject-trusted-ca-bundle"
	CAConfigMapName                = "odh-trusted-ca-bundle"
	CADataFieldName                = "odh-ca-bundle.crt"
)

func ShouldInjectTrustedBundle(ns client.Object) bool {
	if !strings.HasPrefix(ns.GetName(), "openshift-") && !strings.HasPrefix(ns.GetName(), "kube-") &&
		ns.GetName() != "default" && ns.GetName() != "openshift" && !HasCABundleAnnotationDisabled(ns) {
		return true
	}
	return false
}

func HasCABundleAnnotationDisabled(ns client.Object) bool {
	if value, found := ns.GetAnnotations()[injectionOfCABundleAnnotatoion]; found {
		enabled, err := strconv.ParseBool(value)
		return err == nil && !enabled
	}
	return false
}

func AddCABundleConfigMapInAllNamespaces(ctx context.Context, cli client.Client, dscInit *dsci.DSCInitialization) error {
	namespaceList := &corev1.NamespaceList{}
	err := cli.List(ctx, namespaceList)
	if err != nil {
		return err
	}

	for _, ns := range namespaceList.Items {
		if !strings.HasPrefix(ns.Name, "openshift-") && !strings.HasPrefix(ns.Name, "kube-") &&
			ns.Name != "default" && ns.Name != "openshift" {
			if err := CreateOdhTrustedCABundleConfigMap(ctx, cli, ns.Name, dscInit); err != nil {
				return err
			}
		}
	}
	return nil
}

// createOdhTrustedCABundleConfigMap creates a configMap  'odh-trusted-ca-bundle' -- Certificates for the cluster
// trusted CA Cert Bundle.
func CreateOdhTrustedCABundleConfigMap(ctx context.Context, cli client.Client, namespace string, dscInit *dsci.DSCInitialization) error {
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
				"app.kubernetes.io/part-of": "opendatahub-operator",
				// Label required for the Cluster Network Operator(CNO) to inject the cluster trusted CA bundle
				// into .data["ca-bundle.crt"]
				"config.openshift.io/inject-trusted-cabundle": "true",
			},
		},
		// Add the DSCInitialzation specified TrustedCABundle to the odh-ca-bundle.crt data field
		// Additionally, the CNO operator will automatically create and maintain ca-bundle.crt
		//  based on the application of the label 'config.openshift.io/inject-trusted-cabundle'
		Data: map[string]string{CADataFieldName: dscInit.Spec.TrustedCABundle},
	}

	// Create Configmap if doesn't exist
	foundConfigMap := &corev1.ConfigMap{}
	err := cli.Get(ctx, client.ObjectKey{
		Name:      CAConfigMapName,
		Namespace: namespace,
	}, foundConfigMap)
	if err != nil {
		if apierrs.IsNotFound(err) {
			err = cli.Create(ctx, desiredConfigMap)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}

	if foundConfigMap.Data[CADataFieldName] != dscInit.Spec.TrustedCABundle {
		foundConfigMap.Data[CADataFieldName] = dscInit.Spec.TrustedCABundle
		return cli.Update(ctx, foundConfigMap)
	}

	return nil
}

func DeleteOdhTrustedCABundleConfigMap(ctx context.Context, cli client.Client, namespace string) error {
	// Delete Configmap if exists
	foundConfigMap := &corev1.ConfigMap{}
	err := cli.Get(ctx, client.ObjectKey{
		Name:      CAConfigMapName,
		Namespace: namespace,
	}, foundConfigMap)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return err
	}
	return cli.Delete(ctx, foundConfigMap)
}

// IsTrustedCABundleUpdated verifies for a given namespace if the odh-ca-bundle.crt field in cert configmap is updated.
func IsTrustedCABundleUpdated(ctx context.Context, cli client.Client, namespace string, dscInit *dsci.DSCInitialization) (bool, error) {
	usernamespace := &corev1.Namespace{}
	if err := cli.Get(ctx, client.ObjectKey{Name: namespace}, usernamespace); err != nil {
		if apierrs.IsNotFound(err) {
			// if namespace is not found, return true. This is to ensure we reconcile, and check for other namespaces.
			return true, nil
		}
		return false, err
	}

	if HasCABundleAnnotationDisabled(usernamespace) {
		return false, nil
	}

	foundConfigMap := &corev1.ConfigMap{}
	err := cli.Get(ctx, client.ObjectKey{
		Name:      CAConfigMapName,
		Namespace: namespace,
	}, foundConfigMap)

	if err != nil {
		if apierrs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return foundConfigMap.Data[CADataFieldName] != dscInit.Spec.TrustedCABundle, nil
}

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

package gateway

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// Platform sources for the backend TLS trust bundle. OpenShift publishes the
// service CA bundle as a ConfigMap in every namespace, so it is read from the
// gateway namespace (which the operator caches); see
// https://docs.openshift.com/container-platform/latest/security/certificate-types-descriptions.html
const (
	ServiceCAConfigMapName = "openshift-service-ca.crt"
	ServiceCAConfigMapKey  = "service-ca.crt"
)

// Backend TLS trust bundle artifacts. The ConfigMap key follows the Gateway API
// BackendTLSPolicy documentation ("a ConfigMap ... contain[s] a key named
// ca.crt"); istiod accepts both "cacert" and "ca.crt" (verified live on
// Istio 1.30.5, see RHAI-19666).
const (
	BackendTLSCABundleConfigMapName = "data-science-tls-ca-bundle"
	BackendTLSCABundleConfigMapKey  = "ca.crt"
)

// backendTLSTarget identifies a Service the operator's gateways forward to
// over TLS and that therefore becomes the target of a BackendTLSPolicy. Ports
// are the distinct backendRef ports observed across the routes referencing
// the Service; they scope the generated policy to the Service ports that
// actually carry gateway traffic.
type backendTLSTarget struct {
	Namespace string
	Name      string
	Ports     []int32
}

// backendTLSArtifactsKey is the ReconciliationRequest extension key carrying
// the backend TLS artifacts the current reconcile wants to exist. Rendering
// them is not enough to keep the cluster clean: the framework garbage
// collector only deletes owned objects when the instance generation changed,
// and a route disappearing is a watch event that never bumps the
// GatewayConfig generation. The set recorded here is what lets the collector
// delete artifacts whose route is gone (RHAI-19666).
const backendTLSArtifactsKey = "gateway.backendTLSArtifacts"

// backendTLSArtifacts is the desired artifact set of one reconcile, split by
// kind because policies are named after their Service while bundles share one
// name per namespace.
type backendTLSArtifacts struct {
	Policies map[types.NamespacedName]struct{}
	Bundles  map[types.NamespacedName]struct{}
}

// backendTLSDesiredArtifacts returns the artifact set recorded by the backend
// TLS action earlier in this reconcile. The second result is false when the
// action did not record one — it was skipped, or it deliberately preserved the
// previous artifacts because no trust bundle could be assembled — which makes
// the collector fall back to its default generation-based rules.
func backendTLSDesiredArtifacts(rr *odhtypes.ReconciliationRequest) (backendTLSArtifacts, bool) {
	if rr.Extensions == nil {
		return backendTLSArtifacts{}, false
	}
	artifacts, ok := rr.Extensions[backendTLSArtifactsKey].(backendTLSArtifacts)

	return artifacts, ok
}

// recordBackendTLSArtifacts stores the artifacts the current reconcile wants
// to exist, replacing any previous set. Passing no objects records an empty
// set, which makes every existing artifact collectable.
func recordBackendTLSArtifacts(rr *odhtypes.ReconciliationRequest, objects ...client.Object) {
	artifacts := backendTLSArtifacts{
		Policies: map[types.NamespacedName]struct{}{},
		Bundles:  map[types.NamespacedName]struct{}{},
	}
	for _, obj := range objects {
		key := types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
		switch obj.(type) {
		case *gwapiv1.BackendTLSPolicy:
			artifacts.Policies[key] = struct{}{}
		case *corev1.ConfigMap:
			artifacts.Bundles[key] = struct{}{}
		}
	}

	if rr.Extensions == nil {
		rr.Extensions = map[string]any{}
	}
	rr.Extensions[backendTLSArtifactsKey] = artifacts
}

// backendTLSArtifactCollectable reports whether resource is a backend TLS
// artifact the current reconcile no longer wants (first result), and therefore
// must be deleted. The second result is false for every other object — and for
// a reconcile that recorded no desired set — so callers can fall back to their
// own rules.
func backendTLSArtifactCollectable(
	rr *odhtypes.ReconciliationRequest,
	resource unstructured.Unstructured,
) (bool, bool) {
	artifacts, ok := backendTLSDesiredArtifacts(rr)
	if !ok {
		return false, false
	}

	key := types.NamespacedName{Namespace: resource.GetNamespace(), Name: resource.GetName()}
	switch resource.GroupVersionKind().GroupKind() {
	case gvk.BackendTLSPolicy.GroupKind():
		_, desired := artifacts.Policies[key]

		return !desired, true
	case gvk.ConfigMap.GroupKind():
		// Only the trust bundle ConfigMap belongs to this action; the
		// operator's namespaces hold many other ConfigMaps.
		if resource.GetName() != BackendTLSCABundleConfigMapName {
			return false, false
		}
		_, desired := artifacts.Bundles[key]

		return !desired, true
	default:
		return false, false
	}
}

// createBackendTLSPolicies renders a trust-bundle ConfigMap and a
// BackendTLSPolicy for every operator-owned Service backend riding the
// operator's gateways (RHAI-19666). istiod translates each policy into a
// per-host DestinationRule (credentialName + sni) that overrides the wildcard
// insecureSkipVerify rule for that host, so Envoy verifies the backend's
// certificate chain and SAN against the bundle. Backends without a policy
// keep flowing through the wildcard rule.
//
// Rendering is skipped entirely when the BackendTLSPolicy CRD is not served
// (Gateway API < v1.4) or when no trust bundle can be assembled: an empty
// bundle would make istiod reject the policies, and the wildcard rule keeps
// traffic flowing unverified in the meantime.
//
// The artifacts this reconcile wants are recorded on the request so the
// gateway garbage collector can delete the ones a removed route no longer
// implies; see backendTLSArtifactsKey.
func createBackendTLSPolicies(ctx context.Context, rr *odhtypes.ReconciliationRequest, platformCAReader client.Reader) error {
	// Clusters serving Gateway API CRDs older than v1.4 have no
	// BackendTLSPolicy v1; rendering would fail deployment and a watch on the
	// kind would never sync.
	served, err := backendTLSPolicyCRDServed(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to check BackendTLSPolicy CRD presence: %w", err)
	}
	if !served {
		logf.FromContext(ctx).V(1).Info("BackendTLSPolicy CRD is not served; skipping backend TLS policy rendering")
		return nil
	}

	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}

	gatewayNames := []string{GetDefaultGatewayName()}
	for _, ingress := range gatewayConfig.Spec.AdditionalIngresses {
		gatewayNames = append(gatewayNames, ingress.Name)
	}

	backends, err := enumerateBackendTLSTargets(ctx, rr.Client, gatewayNames)
	if err != nil {
		return err
	}
	if len(backends) == 0 {
		// Every artifact this operator owns is now stale: no operator-owned
		// route points at a backend any more.
		recordBackendTLSArtifacts(rr)

		return nil
	}

	serviceCA, err := fetchServiceCA(ctx, rr.Client)
	if err != nil {
		return err
	}
	issuerCA, err := fetchPlatformIssuerCA(ctx, rr.Client, platformCAReader, gatewayConfig.Spec.Certificate)
	if err != nil {
		return err
	}
	bundle := mergePEM(serviceCA, issuerCA)
	if bundle == "" {
		logf.FromContext(ctx).V(1).Info(
			"no trust bundle available for backend TLS verification; skipping BackendTLSPolicy rendering",
			"backends", len(backends))
		return nil
	}

	var resources []client.Object
	bundles := make(map[string]*corev1.ConfigMap)
	for _, backend := range backends {
		bundleConfigMap, ok := bundles[backend.Namespace]
		if !ok {
			bundleConfigMap, err = buildBackendTLSCABundleConfigMap(gatewayConfig, backend.Namespace, bundle, rr.Client.Scheme())
			if err != nil {
				return fmt.Errorf("failed to build backend TLS bundle ConfigMap for namespace %s: %w", backend.Namespace, err)
			}
			bundles[backend.Namespace] = bundleConfigMap
			resources = append(resources, bundleConfigMap)
		}

		policies, err := buildBackendTLSPolicies(ctx, rr.Client, gatewayConfig, backend, bundleConfigMap.Name)
		if err != nil {
			return fmt.Errorf("failed to build BackendTLSPolicy for %s/%s: %w", backend.Namespace, backend.Name, err)
		}
		resources = append(resources, policies...)
	}

	if err := rr.AddResources(resources...); err != nil {
		return fmt.Errorf("failed to add backend TLS resources: %w", err)
	}

	recordBackendTLSArtifacts(rr, resources...)

	return nil
}

// backendTLSPolicyCRDServed reports whether the BackendTLSPolicy CRD exists
// and is Established. It looks the CRD up by its explicit plural name: the
// generic cluster.HasCRD helper derives names as <kind>s.<group>, which is
// wrong for the irregular plural "policies" and would always report absent.
func backendTLSPolicyCRDServed(ctx context.Context, cli client.Client) (bool, error) {
	crd := &unstructured.Unstructured{}
	crd.SetGroupVersionKind(crdGVK)
	if err := cli.Get(ctx, types.NamespacedName{Name: gvk.BackendTLSPolicyCRDName}, crd); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return false, nil
		}
		return false, err
	}

	conditions, found, err := unstructured.NestedSlice(crd.Object, "status", "conditions")
	if err != nil {
		return false, fmt.Errorf("failed to read BackendTLSPolicy CRD conditions: %w", err)
	}
	if !found {
		return false, nil
	}
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if condition["type"] == "Established" && condition["status"] == "True" {
			return true, nil
		}
	}

	return false, nil
}

// crdGVK is the apiextensions CustomResourceDefinition GVK used for direct
// CRD lookups.
var crdGVK = schema.GroupVersionKind{
	Group:   "apiextensions.k8s.io",
	Version: "v1",
	Kind:    "CustomResourceDefinition",
}

// fetchServiceCA returns the OpenShift service CA bundle from the gateway
// namespace. On non-OpenShift clusters it returns an empty string (the source
// does not exist there). A missing ConfigMap on OpenShift is an error: the
// platform always publishes it into every namespace, so its absence is an
// anomaly worth a requeue rather than a silent skip.
func fetchServiceCA(ctx context.Context, cli client.Client) (string, error) {
	if cluster.GetClusterInfo().Type != cluster.ClusterTypeOpenShift {
		return "", nil
	}

	cm := &corev1.ConfigMap{}
	if err := cli.Get(ctx, types.NamespacedName{
		Name:      ServiceCAConfigMapName,
		Namespace: GetGatewayNamespace(),
	}, cm); err != nil {
		return "", fmt.Errorf("failed to read OpenShift service CA ConfigMap %s/%s: %w",
			GetGatewayNamespace(), ServiceCAConfigMapName, err)
	}

	return cm.Data[ServiceCAConfigMapKey], nil
}

// fetchPlatformIssuerCA resolves the CA of the platform cert-manager issuer
// (resolveIssuerRef precedence: GatewayConfig spec, RHAI_ISSUER_REF_* env,
// bootstrap defaults). It returns an empty string — without error — whenever
// the platform PKI is absent or not yet usable: missing issuer CR (CRD absent
// or issuer not created), a non-CA-backed issuer, or a missing/unreadable CA
// Secret. A half-configured PKI degrades to "no platform CA in the bundle"
// instead of blocking gateway reconciliation: an optional trust source must
// never wedge the reconcile.
//
// A namespaced Issuer and its CA Secret are read from the gateway namespace,
// where the gateway's cert-manager Certificates live (cert-manager requires
// the issuer and its secret to share the certificate's namespace). A
// ClusterIssuer's CA Secret lives in the cert-manager controller namespace,
// which is outside the operator's Secret cache on some deployments; that read
// failure is tolerated the same way.
func fetchPlatformIssuerCA(ctx context.Context, cli client.Client, secretReader client.Reader, certSpec *infrav1.CertificateSpec) (string, error) {
	issuerName, issuerKind := resolveIssuerRef(certSpec)

	issuer := &unstructured.Unstructured{}
	secretNamespace := GetGatewayNamespace()
	switch issuerKind {
	case gvk.CertManagerIssuer.Kind:
		issuer.SetGroupVersionKind(gvk.CertManagerIssuer)
		issuer.SetNamespace(secretNamespace)
	default:
		issuer.SetGroupVersionKind(gvk.CertManagerClusterIssuer)
		secretNamespace = certmanager.DefaultBootstrapConfig().CertManagerNamespace
	}
	issuer.SetName(issuerName)

	if err := cli.Get(ctx, types.NamespacedName{
		Name:      issuer.GetName(),
		Namespace: issuer.GetNamespace(),
	}, issuer); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read %s %s: %w", issuerKind, issuerName, err)
	}

	secretName, found, err := unstructured.NestedString(issuer.Object, "spec", "ca", "secretName")
	if err != nil {
		return "", fmt.Errorf("failed to read CA secret reference from %s %s: %w", issuerKind, issuerName, err)
	}
	if !found || secretName == "" {
		// Not a CA-backed issuer; the platform PKI cannot contribute a CA.
		return "", nil
	}

	// The CA Secret is read through an uncached reader: it can live outside
	// the operator's Secret cache (a ClusterIssuer's CA Secret lives in the
	// cert-manager controller namespace), where the cached client fails with
	// a namespace error instead of reading.
	secret := &corev1.Secret{}
	if err := secretReader.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: secretNamespace,
	}, secret); err != nil {
		if k8serr.IsNotFound(err) {
			return "", nil
		}
		// An optional trust source must not wedge the reconcile, but a failed
		// read of an existing PKI is abnormal enough to log visibly.
		logf.FromContext(ctx).Info(
			"failed to read the platform issuer CA Secret; skipping the platform CA in the backend TLS bundle",
			"secret", secretName, "namespace", secretNamespace, "error", err)
		return "", nil
	}

	if ca := secret.Data["ca.crt"]; len(ca) > 0 {
		return string(ca), nil
	}
	if cert := secret.Data["tls.crt"]; len(cert) > 0 {
		return string(cert), nil
	}

	return "", nil
}

// backendTLSAllowedNamespaces returns the namespaces a backend TLS target and
// its artifacts may live in: the gateway namespace and the application
// namespace. This matches both the default listener's allowedRoutes selector
// and the operator's resource cache, so a forged route cannot make the
// operator write outside namespaces it manages.
func backendTLSAllowedNamespaces() map[string]bool {
	return map[string]bool{
		GetGatewayNamespace():             true,
		cluster.GetApplicationNamespace(): true,
	}
}

// enumerateBackendTLSTargets lists the distinct Service backends referenced by
// operator-owned HTTPRoutes attached to the named gateways in the gateway
// namespace. Operator-owned means the route carries the platform part-of label
// (platform.opendatahub.io/part-of), lives in an allowed namespace, and
// references the gateway with the default Gateway group/kind; foreign user
// routes are excluded. The result is deterministic: backends sorted by
// namespace then name, ports sorted ascending.
func enumerateBackendTLSTargets(ctx context.Context, cli client.Client, gatewayNames []string) ([]backendTLSTarget, error) {
	if len(gatewayNames) == 0 {
		return nil, nil
	}

	gatewaySet := make(map[string]bool, len(gatewayNames))
	for _, name := range gatewayNames {
		gatewaySet[name] = true
	}

	routeList := &gwapiv1.HTTPRouteList{}
	if err := cli.List(ctx, routeList); err != nil {
		return nil, fmt.Errorf("failed to list HTTPRoutes: %w", err)
	}

	gatewayNamespace := GetGatewayNamespace()
	allowedNamespaces := backendTLSAllowedNamespaces()

	type backendKey struct {
		namespace string
		name      string
	}
	backends := make(map[backendKey]*backendTLSTarget)

	for _, route := range routeList.Items {
		// Only routes the operator rendered carry the platform part-of label,
		// and only routes in the operator's namespaces may direct the operator
		// to render backend TLS artifacts there.
		if route.Labels[labels.PlatformPartOf] == "" || !allowedNamespaces[route.Namespace] {
			continue
		}

		attached := false
		for _, ref := range route.Spec.ParentRefs {
			if ref.Group != nil && string(*ref.Group) != gvk.KubernetesGateway.Group {
				continue
			}
			if ref.Kind != nil && string(*ref.Kind) != gvk.KubernetesGateway.Kind {
				continue
			}
			// Gateway API resolves an unset parentRef namespace to the
			// route's own namespace, not the gateway's: a route in the
			// applications namespace without an explicit namespace never
			// attaches to a gateway in the gateway namespace, so it must not
			// pull a policy in.
			refNamespace := route.Namespace
			if ref.Namespace != nil {
				refNamespace = string(*ref.Namespace)
			}
			if gatewaySet[string(ref.Name)] && refNamespace == gatewayNamespace {
				attached = true
				break
			}
		}
		if !attached {
			continue
		}

		for _, rule := range route.Spec.Rules {
			for _, backendRef := range rule.BackendRefs {
				ref := backendRef.BackendObjectReference

				// Default kind is Service per the Gateway API; only core
				// Service backends are eligible for a BackendTLSPolicy here.
				if ref.Kind != nil && string(*ref.Kind) != "Service" {
					continue
				}
				if ref.Group != nil && string(*ref.Group) != "" {
					continue
				}

				namespace := route.Namespace
				if ref.Namespace != nil {
					namespace = string(*ref.Namespace)
				}
				// The policy and its bundle ConfigMap must live in the
				// backend's namespace; restrict to namespaces the operator
				// manages and caches.
				if !allowedNamespaces[namespace] {
					continue
				}

				key := backendKey{namespace: namespace, name: string(ref.Name)}
				backend, ok := backends[key]
				if !ok {
					backend = &backendTLSTarget{Namespace: namespace, Name: string(ref.Name)}
					backends[key] = backend
				}

				if ref.Port != nil {
					port := *ref.Port
					if !slices.Contains(backend.Ports, port) {
						backend.Ports = append(backend.Ports, port)
					}
				}
			}
		}
	}

	result := make([]backendTLSTarget, 0, len(backends))
	for _, backend := range backends {
		slices.Sort(backend.Ports)
		result = append(result, *backend)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Namespace != result[j].Namespace {
			return result[i].Namespace < result[j].Namespace
		}
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// buildBackendTLSCABundleConfigMap builds the operator-owned trust bundle
// ConfigMap for a backend namespace. One ConfigMap is shared by every backend
// in the same namespace: BackendTLSPolicy requires the referenced ConfigMap to
// live in the policy's own namespace.
func buildBackendTLSCABundleConfigMap(
	owner *serviceApi.GatewayConfig,
	namespace, bundle string,
	scheme *runtime.Scheme,
) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BackendTLSCABundleConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				labels.PlatformPartOf: PartOfGatewayConfig,
			},
		},
		Data: map[string]string{
			BackendTLSCABundleConfigMapKey: bundle,
		},
	}
	if err := controllerutil.SetControllerReference(owner, configMap, scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference on backend TLS bundle ConfigMap: %w", err)
	}

	return configMap, nil
}

// buildBackendTLSPolicies builds the BackendTLSPolicies for one backend
// Service. The validation hostname doubles as the SNI and the expected SAN,
// which is the shape verified live against istiod 1.30.5 during the
// RHAI-19666 spike (an authority-derived SAN never matches backend
// service-DNS certificates). The service-DNS suffix matches the service CA
// certificate SANs and the operator's own cert-manager Certificate DNS names
// (.svc.cluster.local).
//
// When the Service exposes more than one port, one policy per referenced port
// is generated with a sectionName pinning that port, so non-TLS ports (for
// example a plaintext metrics port) keep the wildcard rule's behaviour
// instead of being forced into TLS origination. Single-port Services (or a
// missing Service, for a backend not yet deployed) get one service-wide
// policy.
func buildBackendTLSPolicies(
	ctx context.Context,
	cli client.Client,
	owner *serviceApi.GatewayConfig,
	backend backendTLSTarget,
	bundleConfigMapName string,
) ([]client.Object, error) {
	service := &corev1.Service{}
	serviceLookupFailed := false
	if err := cli.Get(ctx, types.NamespacedName{
		Namespace: backend.Namespace,
		Name:      backend.Name,
	}, service); err != nil {
		if !k8serr.IsNotFound(err) {
			return nil, fmt.Errorf("failed to read backend Service %s/%s: %w", backend.Namespace, backend.Name, err)
		}
		serviceLookupFailed = true
	}

	if serviceLookupFailed || len(service.Spec.Ports) <= 1 {
		policy, err := buildBackendTLSPolicy(owner, backend, "", bundleConfigMapName, cli.Scheme())
		if err != nil {
			return nil, err
		}
		return []client.Object{policy}, nil
	}

	portNames := make(map[int32]string, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		portNames[port.Port] = port.Name
	}

	var policies []client.Object
	for _, referencedPort := range backend.Ports {
		portName, ok := portNames[referencedPort]
		if !ok {
			// The route references a port the Service does not expose; there
			// is nothing to verify for it.
			continue
		}
		policy, err := buildBackendTLSPolicy(owner, backend, portName, bundleConfigMapName, cli.Scheme())
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}

	return policies, nil
}

// buildBackendTLSPolicy builds a single BackendTLSPolicy. An empty sectionName
// targets the whole Service; otherwise the policy is named
// <service>-<sectionName> and pinned to that port.
func buildBackendTLSPolicy(
	owner *serviceApi.GatewayConfig,
	backend backendTLSTarget,
	sectionName, bundleConfigMapName string,
	scheme *runtime.Scheme,
) (*gwapiv1.BackendTLSPolicy, error) {
	coreGroup := ""
	serviceKind := "Service"
	configMapKind := "ConfigMap"

	name := backend.Name
	var targetSectionName *gwapiv1.SectionName
	if sectionName != "" {
		// A dot separates the Service name from the port name: Service names
		// cannot contain dots, so a per-port policy can never collide with
		// another Service's policy.
		name = backend.Name + "." + sectionName
		targetSectionName = (*gwapiv1.SectionName)(&sectionName)
	}

	policy := &gwapiv1.BackendTLSPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: backend.Namespace,
			Labels: map[string]string{
				labels.PlatformPartOf: PartOfGatewayConfig,
			},
		},
		Spec: gwapiv1.BackendTLSPolicySpec{
			TargetRefs: []gwapiv1.LocalPolicyTargetReferenceWithSectionName{{
				LocalPolicyTargetReference: gwapiv1.LocalPolicyTargetReference{
					Group: gwapiv1.Group(coreGroup),
					Kind:  gwapiv1.Kind(serviceKind),
					Name:  gwapiv1.ObjectName(backend.Name),
				},
				SectionName: targetSectionName,
			}},
			Validation: gwapiv1.BackendTLSPolicyValidation{
				Hostname: gwapiv1.PreciseHostname(fmt.Sprintf("%s.%s.svc.cluster.local", backend.Name, backend.Namespace)),
				CACertificateRefs: []gwapiv1.LocalObjectReference{{
					Group: gwapiv1.Group(coreGroup),
					Kind:  gwapiv1.Kind(configMapKind),
					Name:  gwapiv1.ObjectName(bundleConfigMapName),
				}},
			},
		},
	}
	if err := controllerutil.SetControllerReference(owner, policy, scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference on BackendTLSPolicy: %w", err)
	}

	return policy, nil
}

// pemBlockDelimiters bound a single PEM certificate block.
const (
	pemBeginBlock = "-----BEGIN CERTIFICATE-----"
	pemEndBlock   = "-----END CERTIFICATE-----"
)

// mergePEM concatenates the certificate blocks of the given bundles in order,
// dropping duplicate blocks. Bundles without complete blocks are ignored, so
// mergePEM("", ...) is a no-op and an all-empty input yields an empty string.
func mergePEM(bundles ...string) string {
	seen := make(map[string]bool)
	var merged []string

	for _, bundle := range bundles {
		for _, block := range splitPEMBlocks(bundle) {
			if seen[block] {
				continue
			}
			seen[block] = true
			merged = append(merged, block)
		}
	}

	return strings.Join(merged, "")
}

// splitPEMBlocks splits a PEM bundle into its complete certificate blocks,
// each normalized to end with a newline.
func splitPEMBlocks(bundle string) []string {
	var blocks []string
	current := make([]string, 0, 2)
	inBlock := false

	for line := range strings.SplitSeq(bundle, "\n") {
		switch {
		case strings.HasPrefix(line, pemBeginBlock):
			current = append(current[:0], line)
			inBlock = true
		case inBlock:
			current = append(current, line)
			if strings.HasPrefix(line, pemEndBlock) {
				blocks = append(blocks, strings.Join(current, "\n")+"\n")
				current = nil
				inBlock = false
			}
		}
	}

	return blocks
}

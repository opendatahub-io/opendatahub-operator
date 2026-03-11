package common

// +kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=get;list;watch;create;patch;update

// +kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="core",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="core",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="core",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="core",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="core",resources=namespaces,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=get;list;watch;create;update;patch;delete

// Events
// TODO: this should be removed once controller-runtime >= 0.23.0
// +kubebuilder:rbac:groups="core",resources=events,verbs=get;create;watch;update;list;patch
// +kubebuilder:rbac:groups="events.k8s.io",resources=events,verbs=list;watch;patch;get

// RBAC
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=create;watch;list
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=create;watch;list;get;
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;patch;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterrolebindings,verbs=get;list;watch;create;patch;update;delete

// Escalation and bind permission are needed for the dependencies role and clusterrole,
// to allow the cloud manager to update them without have all underlying permissions.
// Security risks are mitigated by the fact that the resourceNames are specified.
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;patch;update;delete;bind;escalate,resourceNames=cert-manager-operator-controller-manager-role
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;patch;update;delete;bind;escalate,resourceNames=openshift-lws-operator-role
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;patch;update;delete;bind;escalate,resourceNames=servicemesh-operator3-role

// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=get;list;watch;patch;update;delete;bind;escalate,resourceNames=cert-manager-operator-controller-manager-clusterrole;cert-manager-operator-metrics-reader
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=get;list;watch;patch;update;delete;bind;escalate,resourceNames=openshift-lws-operator-clusterrole
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=get;list;watch;patch;update;delete;bind;escalate,resourceNames=servicemesh-operator3-clusterrole;metrics-reader

// cert-manager
// +kubebuilder:rbac:groups="operator.openshift.io",resources=certmanagers,verbs=get;list;watch;create;patch;update;delete

// sail-operator
// +kubebuilder:rbac:groups="sail-operator.io",resources=istios,verbs=get;list;watch;create;patch;update;delete

// Webhook annotations for sail-operator workaround (OSSM-12397)
// TODO(OSSM-12397): Remove once the sail-operator ships a fix.
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;watch;patch

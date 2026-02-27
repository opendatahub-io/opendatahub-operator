package azure

// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=azurekubernetesengines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=azurekubernetesengines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=azurekubernetesengines/finalizers,verbs=update

// +kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=get;list;watch;create;patch;update

// +kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="core",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="core",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="core",resources=services,verbs=get;list;watch;create;update;patch;delete

// RBAC
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;patch;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;patch;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=get;list;watch;create;patch;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterrolebindings,verbs=get;list;watch;create;patch;update;delete

// cert-manager
// +kubebuilder:rbac:groups="operator.openshift.io",resources=certmanagers,verbs=get;list;watch;create;patch;update;delete
// +kubebuilder:rbac:groups="config.openshift.io",resources=infrastructures,verbs=get;list;watch;create;patch;update;delete

// sail-operator
// +kubebuilder:rbac:groups="sail-operator.io",resources=istios,verbs=get;list;watch;create;patch;update;delete

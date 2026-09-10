package modules

// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platforms,verbs=get;list;watch
// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platforms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platforms/finalizers,verbs=update

// AIGateway: new mega module
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=aigateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=aigateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=aigateways/finalizers,verbs=update

// Dashboard: module-based architecture
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=dashboards,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=dashboards/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=dashboards/finalizers,verbs=update

// Kserve module
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=kserves,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=kserves/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=kserves/finalizers,verbs=update

// MLflowOperator module
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=mlflowoperators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=mlflowoperators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=mlflowoperators/finalizers,verbs=update

// MCPLifecycleOperator module
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=mcplifecycleoperators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=mcplifecycleoperators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=mcplifecycleoperators/finalizers,verbs=update

// FeastOperator module
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=feastoperators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=feastoperators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=feastoperators/finalizers,verbs=update

// Workbenches module
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=workbenches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=workbenches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=workbenches/finalizers,verbs=update

// OGX module
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=ogxs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=ogxs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=ogxs/finalizers,verbs=update

// SparkOperator module
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=sparkoperators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=sparkoperators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=sparkoperators/finalizers,verbs=update
// AIHub module (model-registry)
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=aihubs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=aihubs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=aihubs/finalizers,verbs=update

// Monitoring module
// +kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=monitorings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=monitorings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=monitorings/finalizers,verbs=update

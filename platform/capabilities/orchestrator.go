package capabilities

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/opendatahub-io/odh-platform/controllers"
	"github.com/opendatahub-io/odh-platform/controllers/authzctrl"
	"github.com/opendatahub-io/odh-platform/controllers/routingctrl"
	"github.com/opendatahub-io/odh-platform/pkg/authorization"
	"github.com/opendatahub-io/odh-platform/pkg/platform"
	"github.com/opendatahub-io/odh-platform/pkg/routing"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups="route.openshift.io",resources=routes,verbs=*
// +kubebuilder:rbac:groups="route.openshift.io",resources=routes/custom-host,verbs=*
// +kubebuilder:rbac:groups="networking.istio.io",resources=virtualservices,verbs=*
// +kubebuilder:rbac:groups="networking.istio.io",resources=gateways,verbs=*
// +kubebuilder:rbac:groups="networking.istio.io",resources=destinationrules,verbs=*

// +kubebuilder:rbac:groups=authorino.kuadrant.io,resources=authconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=security.istio.io,resources=authorizationpolicies,verbs=get;list;watch;create;update;patch;delete

// PlatformOrchestrator is responsible for managing the lifecycle of platform capabilities and respective controllers.
type PlatformOrchestrator struct {
	log     logr.Logger
	authz   activator[authorization.ProviderConfig, platform.ProtectedResource]
	routing activator[routing.IngressConfig, platform.RoutingTarget]
}

func NewPlatformOrchestrator(log logr.Logger, manager controllerruntime.Manager) (*PlatformOrchestrator, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(manager.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client for PlatformOrchestrator: %w", err)
	}

	p := &PlatformOrchestrator{
		log: log,
		authz: activator[authorization.ProviderConfig, platform.ProtectedResource]{
			log:             log.WithValues("capability", "authz"),
			mgr:             manager,
			discoveryClient: discoveryClient,
		},
		routing: activator[routing.IngressConfig, platform.RoutingTarget]{
			log:             log.WithValues("capability", "routing"),
			mgr:             manager,
			discoveryClient: discoveryClient,
		},
	}
	return p, nil
}

// ToggleRouting ensures that only the controllers for currently desired routing targets are active.
func (p *PlatformOrchestrator) ToggleRouting(ctx context.Context, cli client.Client, config routing.IngressConfig, refs ...platform.RoutingTarget) error {
	p.routing.deactivateStaleCtrls(refs...)

	createCtrl := func(ref platform.RoutingTarget) activableCtrl[routing.IngressConfig] {
		return routingctrl.New(cli, p.log, ref, config)
	}

	updateCtrl := func(ctrl activableCtrl[routing.IngressConfig]) {
		ctrl.Activate(config)
	}

	return p.routing.activateOrNewCtrl(ctx, createCtrl, updateCtrl, refs...)
}

// ToggleAuthorization ensures that only the controllers for currently desired protected resources are active.
func (p *PlatformOrchestrator) ToggleAuthorization(ctx context.Context, cli client.Client, config authorization.ProviderConfig, refs ...platform.ProtectedResource) error {
	p.authz.deactivateStaleCtrls(refs...)

	createCtrl := func(ref platform.ProtectedResource) activableCtrl[authorization.ProviderConfig] {
		return authzctrl.New(cli, p.log, ref, config)
	}

	activateCtrl := func(ctrl activableCtrl[authorization.ProviderConfig]) {
		ctrl.Activate(config)
	}

	return p.authz.activateOrNewCtrl(ctx, createCtrl, activateCtrl, refs...)
}

// activableCtrl allows to activate or deactivate a controller and wire it with controller-runtime manager.
type activableCtrl[ConfigType any] interface {
	controllers.Activable[ConfigType]
	Name() string
	SetupWithManager(mgr controllerruntime.Manager) error
}

type hasResourceReference interface {
	GetResourceReference() platform.ResourceReference
}

// activator manages the lifecycle of controllers for a given capability.
// It ensures that controllers are started with the right configuration when required and deactivated when no longer needed.
type activator[Config any, Res hasResourceReference] struct {
	mu              sync.RWMutex
	ctrls           map[platform.ResourceReference]activableCtrl[Config]
	log             logr.Logger
	mgr             controllerruntime.Manager
	discoveryClient discovery.DiscoveryInterface
}

// activateCtrlFn is a function that updates the controller with the latest configuration and activates it.
type activateCtrlFn[ConfigType any] func(activableCtrl[ConfigType])

// createCtrlFn is a function that creates a new controller instance for a given resource reference.
type createCtrlFn[ConfigType any, ResType hasResourceReference] func(ref ResType) activableCtrl[ConfigType]

// activateOrNewCtrl attempts to activate a controller which is already watching the given resource reference with updated configuration or
// will create a new instance if not existing yet.
func (a *activator[Config, Res]) activateOrNewCtrl(ctx context.Context, create createCtrlFn[Config, Res], activate activateCtrlFn[Config], refs ...Res) error {
	var errSetup []error
	var wg sync.WaitGroup

	for _, ref := range refs {
		wg.Add(1)

		currentRef := ref
		resourceReference := currentRef.GetResourceReference()

		// Resolve watches for all requested components in parallel, so they do not wait for others
		// if their CRDs are not persisted yet in the cluster.
		go func() {
			defer wg.Done()

			// TODO(nice-to-have): encapsulate map with mutex so RW is uniformly handled without potential concurrent access.
			a.mu.RLock()
			ctrl, watchExists := a.ctrls[resourceReference]
			a.mu.RUnlock()

			if !watchExists {
				resourceExists := func(ctx context.Context) (bool, error) {
					resources, err := a.discoveryClient.ServerResourcesForGroupVersion(resourceReference.GroupVersion().String())
					if err != nil {
						return false, client.IgnoreNotFound(err)
					}

					return resources.Size() > 0, nil
				}

				if errResWait := wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 10*time.Second, true, resourceExists); errResWait != nil {
					errSetup = append(errSetup, fmt.Errorf("failed to wait for resource '%s' to be available: %w", resourceReference.GroupVersionKind.String(), errResWait))
					return
				}

				controller := create(currentRef)
				if errStart := controller.SetupWithManager(a.mgr); errStart != nil {
					errSetup = append(errSetup, fmt.Errorf("failed to setup controller %s: %w", controller.Name(), errStart))
					return
				}

				a.mu.Lock()
				a.ctrls[resourceReference] = controller
				a.mu.Unlock()
			} else {
				activate(ctrl)
			}
		}()
	}

	wg.Wait()

	return errors.Join(errSetup...)
}

// deactivateStaleCtrls deactivates controllers that are not required anymore, meaning there are no resource references
// previously watched that are still required. This can happen when a component has been deactivated.
func (a *activator[Config, Res]) deactivateStaleCtrls(refs ...Res) {
	if a.ctrls == nil {
		a.ctrls = make(map[platform.ResourceReference]activableCtrl[Config])
	}

	ctrlState := make(map[platform.ResourceReference]bool)
	for objectRef := range a.ctrls {
		ctrlState[objectRef] = false
	}

	for _, ref := range refs {
		ctrlState[ref.GetResourceReference()] = true
	}

	for objectRef, isActive := range ctrlState {
		if !isActive {
			a.ctrls[objectRef].Deactivate()
		}
	}
}

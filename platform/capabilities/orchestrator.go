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
	authz   capabilityActivator[platform.ProtectedResource]
	routing capabilityActivator[platform.RoutingTarget]
}

func NewPlatformOrchestrator(log logr.Logger, manager controllerruntime.Manager) (*PlatformOrchestrator, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(manager.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client for PlatformOrchestrator: %w", err)
	}

	p := &PlatformOrchestrator{
		log: log,
		authz: capabilityActivator[platform.ProtectedResource]{
			log:             log.WithValues("capibility", "authz"),
			mgr:             manager,
			discoveryClient: discoveryClient,
		},
		routing: capabilityActivator[platform.RoutingTarget]{
			log:             log.WithValues("capibility", "routing"),
			mgr:             manager,
			discoveryClient: discoveryClient,
		},
	}
	return p, nil
}

func (p *PlatformOrchestrator) ToggleRouting(ctx context.Context, cli client.Client, config routing.PlatformRoutingConfiguration, refs ...platform.RoutingTarget) error {
	p.routing.deactivateStaleCtrls(refs...)

	createCtrl := func(ref platform.RoutingTarget) activableCtrl {
		return routingctrl.New(cli, p.log, ref, config)
	}

	return p.routing.activateOrNewCtrl(ctx, createCtrl, refs...)
}

func (p *PlatformOrchestrator) ToggleAuthorization(ctx context.Context, cli client.Client, config authzctrl.PlatformAuthorizationConfig, refs ...platform.ProtectedResource) error {
	p.authz.deactivateStaleCtrls(refs...)

	createCtrl := func(ref platform.ProtectedResource) activableCtrl {
		return authzctrl.New(cli, p.log, ref, config)
	}

	return p.authz.activateOrNewCtrl(ctx, createCtrl, refs...)
}

type hasResourceReference interface {
	GetResourceReference() platform.ResourceReference
}

type activableCtrl interface {
	controllers.Activable
	Name() string
	SetupWithManager(mgr controllerruntime.Manager) error
}

type createCtrl[T hasResourceReference] func(ref T) activableCtrl

type capabilityActivator[T hasResourceReference] struct {
	mu              sync.RWMutex
	log             logr.Logger
	mgr             controllerruntime.Manager
	ctrls           map[platform.ResourceReference]activableCtrl
	discoveryClient discovery.DiscoveryInterface
}

// deactivateStaleCtrls deactivates controllers that are not required anymore, meaning there are no resource references
// previously watched that are still required. This can happen when a component has been deactivated.
func (c *capabilityActivator[T]) deactivateStaleCtrls(currentRefs ...T) {
	if c.ctrls == nil {
		c.ctrls = make(map[platform.ResourceReference]activableCtrl)
	}

	ctrlState := make(map[platform.ResourceReference]bool)
	for objectRef := range c.ctrls {
		ctrlState[objectRef] = false
	}

	for _, ref := range currentRefs {
		ctrlState[ref.GetResourceReference()] = true
	}

	for objectRef, active := range ctrlState {
		if !active {
			c.ctrls[objectRef].Deactivate()
		}
	}
}

func (c *capabilityActivator[T]) activateOrNewCtrl(ctx context.Context, createCtrlFunc createCtrl[T], currentRefs ...T) error {
	var errSetup []error

	var wg sync.WaitGroup

	for _, ref := range currentRefs {
		wg.Add(1)

		currentRef := ref
		resourceReference := currentRef.GetResourceReference()

		// Resolve watches for all requested components in parallel, so they do not wait for others if their CRDs are not yet
		// persisted in the cluster.
		go func() {
			defer wg.Done()

			// TODO(nice-to-have): encapsulate map with mutex so RW is uniformly handled without potential concurrent access.
			c.mu.Lock()
			ctrl, watchExists := c.ctrls[resourceReference]
			c.mu.Unlock()

			if !watchExists {
				resourceExists := func(ctx context.Context) (bool, error) {
					resources, err := c.discoveryClient.ServerResourcesForGroupVersion(resourceReference.GroupVersion().String())
					if err != nil {
						return false, client.IgnoreNotFound(err)
					}

					return resources.Size() > 0, nil
				}

				if errResWait := wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 10*time.Second, true, resourceExists); errResWait != nil {
					errSetup = append(errSetup, fmt.Errorf("failed to wait for resource '%s' to be available: %w", resourceReference.GroupVersionKind.String(), errResWait))
					return
				}

				controller := createCtrlFunc(currentRef)
				if errStart := controller.SetupWithManager(c.mgr); errStart != nil {
					errSetup = append(errSetup, fmt.Errorf("failed to setup controller %s: %w", controller.Name(), errStart))
					return
				}

				c.mu.Lock()
				c.ctrls[resourceReference] = controller
				c.mu.Unlock()
			} else {
				ctrl.Activate()
			}
		}()
	}

	wg.Wait()

	return errors.Join(errSetup...)
}

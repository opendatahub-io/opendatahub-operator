//nolint:dupl //reason temporary to make things working, refactor later
package capabilities

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/opendatahub-io/odh-platform/controllers"
	"github.com/opendatahub-io/odh-platform/controllers/authzctrl"
	"github.com/opendatahub-io/odh-platform/controllers/routingctrl"
	"github.com/opendatahub-io/odh-platform/pkg/platform"
	"github.com/opendatahub-io/odh-platform/pkg/spi"
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

type PlatformOrchestrator struct {
	Log     logr.Logger
	Manager controllerruntime.Manager
	// TODO split by capability? the core logic for setting stuff up is very similar, differs by types and ctrl creation func
	authz   map[platform.ObjectReference]controllers.Activable
	routing map[platform.ObjectReference]controllers.Activable
}

func (p *PlatformOrchestrator) StartRouting(ctx context.Context, cli client.Client, config spi.PlatformRoutingConfiguration, refs ...platform.RoutingTarget) error {
	if p.routing == nil {
		p.routing = make(map[platform.ObjectReference]controllers.Activable)
	}

	for objectRef, controller := range p.routing {
		requiredToWatch := false
		for _, target := range refs {
			if target.ObjectReference == objectRef {
				requiredToWatch = true
				break
			}
		}

		if !requiredToWatch {
			controller.Deactivate()
		}
	}

	var errSetup []error
	for _, routingTarget := range refs {
		ctrl, alreadyExists := p.routing[routingTarget.ObjectReference]

		if !alreadyExists {
			component := spi.RoutingComponent{RoutingTarget: routingTarget}
			// TODO(mvp): retry until CRD/object reference exists
			// TODO(mvp): non-blocking wait.PollUntilContextTimeout()
			controller := routingctrl.New(cli, p.Log, component, config)
			errStart := controller.SetupWithManager(p.Manager)
			if errStart != nil {
				errSetup = append(errSetup, fmt.Errorf("failed to setup routing controller: %w", errStart))
				continue
			}

			p.routing[routingTarget.ObjectReference] = controller
		} else {
			ctrl.Activate()
		}
	}

	return errors.Join(errSetup...)
}

func (p *PlatformOrchestrator) StartAuthorization(ctx context.Context, cli client.Client,
	config authzctrl.PlatformAuthorizationConfig, refs ...platform.ProtectedResource) error {
	if p.authz == nil {
		p.authz = make(map[platform.ObjectReference]controllers.Activable)
	}

	for objectRef, controller := range p.authz {
		requiredToWatch := false
		for _, target := range refs {
			if target.ObjectReference == objectRef {
				requiredToWatch = true
				break
			}
		}

		if !requiredToWatch {
			controller.Deactivate()
		}
	}

	var errSetup []error
	for _, protectedResource := range refs {
		ctrl, alreadyExists := p.routing[protectedResource.ObjectReference]

		if !alreadyExists {
			component := spi.AuthorizationComponent{ProtectedResource: protectedResource}
			// TODO(mvp): retry until CRD/object reference exists
			// TODO(mvp): non-blocking wait.PollUntilContextTimeout()
			controller := authzctrl.New(cli, p.Log, component, config)
			errStart := controller.SetupWithManager(p.Manager)
			if errStart != nil {
				errSetup = append(errSetup, fmt.Errorf("failed to setup authorization controller: %w", errStart))
				continue
			}

			p.authz[protectedResource.ObjectReference] = controller
		} else {
			ctrl.Activate()
		}
	}

	return errors.Join(errSetup...)
}

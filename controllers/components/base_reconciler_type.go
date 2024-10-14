package components

import (
	"context"
	"github.com/go-logr/logr"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type Action interface {
	Execute(ctx context.Context, r *BaseReconciler, obj client.Object) error
}

type BaseReconciler struct {
	Client       client.Client
	Scheme       *runtime.Scheme
	Actions      []Action
	Log          logr.Logger
	Manager      manager.Manager
	Controller   controller.Controller
	Recorder     record.EventRecorder
	Platform     cluster.Platform
	DSCinstance  *dscv1.DataScienceCluster
	DSCIinstance *dsciv1.DSCInitialization
	entryPath    string
}

func NewBaseReconciler(mgr manager.Manager, name string) *BaseReconciler {
	return &BaseReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName(name),
		Manager:  mgr,
		Recorder: mgr.GetEventRecorderFor(name),
		Platform: cluster.GetRelease().Name,
	}
}

func (r *BaseReconciler) AddAction(action Action) {
	r.Actions = append(r.Actions, action)
}

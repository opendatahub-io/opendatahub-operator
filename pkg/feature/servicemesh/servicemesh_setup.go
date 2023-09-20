package servicemesh

import (
	"github.com/hashicorp/go-multierror"
	v1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/pkg/errors"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = ctrlLog.Log.WithName("service-mesh")

type ServiceMeshInitializer struct {
	*v1.DSCInitializationSpec
	defineFeatures DefineFeatures
	Features       []*feature.Feature
}

type DefineFeatures func(s *ServiceMeshInitializer) error

func NewServiceMeshInitializer(spec *v1.DSCInitializationSpec, def DefineFeatures) *ServiceMeshInitializer {
	return &ServiceMeshInitializer{
		DSCInitializationSpec: spec,
		defineFeatures:        def,
	}
}

// Prepare performs validation of the spec and ensures all resources,
// such as Features and their templates, are processed and initialized
// before proceeding with the actual cluster set-up.
func (s *ServiceMeshInitializer) Prepare() error {
	log.Info("Initializing Service Mesh configuration")

	serviceMeshSpec := &s.DSCInitializationSpec.ServiceMesh

	if valid, reason := serviceMeshSpec.IsValid(); !valid {
		return errors.New(reason)
	}

	return s.defineFeatures(s)
}

func (s *ServiceMeshInitializer) Apply() error {
	var applyErrors *multierror.Error

	for _, f := range s.Features {
		err := f.Apply()
		applyErrors = multierror.Append(applyErrors, err)
	}

	return applyErrors.ErrorOrNil()
}

// Delete executes registered clean-up tasks in the opposite order they were initiated (following a stack structure).
// For instance, this allows for the unpatching of Service Mesh Control Plane before its deletion.
// This approach assumes that Features are either instantiated in the correct sequence
// or are self-contained.
func (s *ServiceMeshInitializer) Delete() error {
	var cleanupErrors *multierror.Error
	for i := len(s.Features) - 1; i >= 0; i-- {
		log.Info("cleanup", "name", s.Features[i].Name)
		cleanupErrors = multierror.Append(cleanupErrors, s.Features[i].Cleanup())
	}

	return cleanupErrors.ErrorOrNil()
}

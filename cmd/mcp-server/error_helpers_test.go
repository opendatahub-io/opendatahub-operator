package main

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func newErrorClient(err error) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return err
			},
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return err
			},
		}).
		Build()
}

func newForbiddenClient() client.Client {
	return newErrorClient(k8serr.NewForbidden(
		schema.GroupResource{Resource: "resources"},
		"", fmt.Errorf("forbidden")))
}

func newNoMatchClient() client.Client {
	return newErrorClient(&meta.NoKindMatchError{
		GroupKind:        schema.GroupKind{Group: "dscinitialization.opendatahub.io", Kind: "DSCInitialization"},
		SearchedVersions: []string{"v1"},
	})
}

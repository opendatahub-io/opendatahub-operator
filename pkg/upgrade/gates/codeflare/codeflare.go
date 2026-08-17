package codeflare

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

func Check(ctx context.Context, reader client.Reader, _, _ string) error {
	blocking := &UpgradeBlockedError{}

	if present, err := hasCodeFlareCR(ctx, reader); err != nil {
		return err
	} else if present {
		blocking.CodeFlareCRPresent = true
	}

	appWrappers, err := countAppWrappers(ctx, reader)
	if err != nil {
		return err
	}
	blocking.AppWrappers = appWrappers

	if !blocking.CodeFlareCRPresent && blocking.AppWrappers == 0 {
		return nil
	}

	return blocking
}

func hasCodeFlareCR(ctx context.Context, reader client.Reader) (bool, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.CodeFlare)

	err := reader.Get(ctx, client.ObjectKey{Name: componentApi.CodeFlareInstanceName}, obj)
	switch {
	case k8serr.IsNotFound(err), meta.IsNoMatchError(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("getting CodeFlare CR: %w", err)
	default:
		return true, nil
	}
}

func countAppWrappers(ctx context.Context, reader client.Reader) (int, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.AppWrapper)

	err := reader.List(ctx, list)
	switch {
	case meta.IsNoMatchError(err):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("listing AppWrappers: %w", err)
	default:
		return len(list.Items), nil
	}
}

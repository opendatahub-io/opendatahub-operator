package secretgenerator_test

import (
	"context"
	"testing"

	"github.com/onsi/gomega/gstruct"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/secretgenerator"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"

	. "github.com/onsi/gomega"
)

//nolint:ireturn
func newFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

func TestGenerateSecret(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	secretName := "foo"
	secretNs := "ns"

	// secret expected to be found
	existingSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNs,
			Labels: map[string]string{
				"foo": "bar",
			},
			Annotations: map[string]string{
				annotations.SecretNameAnnotation: "bar",
				annotations.SecretTypeAnnotation: "random",
			},
		},
	}

	// secret to be generated
	generatedSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName + "-generated",
			Namespace: secretNs,
		},
	}

	cli := newFakeClient(&existingSecret)

	r := secretgenerator.SecretGeneratorReconciler{
		Client: cli,
	}

	_, err := r.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      existingSecret.Name,
			Namespace: existingSecret.Namespace,
		},
	})

	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Get(ctx, client.ObjectKeyFromObject(&generatedSecret), &generatedSecret)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(generatedSecret.OwnerReferences).To(HaveLen(1))
	g.Expect(generatedSecret.OwnerReferences[0]).To(
		gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Name":       Equal(existingSecret.Name),
			"Kind":       Equal(existingSecret.Kind),
			"APIVersion": Equal(existingSecret.APIVersion),
		}),
	)

	g.Expect(generatedSecret.StringData).To(
		HaveKey(existingSecret.Annotations[annotations.SecretNameAnnotation]))
	g.Expect(generatedSecret.Labels).To(
		gstruct.MatchAllKeys(gstruct.Keys{
			"foo": Equal("bar"),
		}),
	)
}

func TestExistingSecret(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	secretName := "foo"
	secretNs := "ns"

	// secret expected to be found
	existingSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNs,
			Labels: map[string]string{
				"foo": "bar",
			},
			Annotations: map[string]string{
				annotations.SecretNameAnnotation: "bar",
				annotations.SecretTypeAnnotation: "random",
			},
		},
	}

	// secret to be generated
	generatedSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName + "-generated",
			Namespace: secretNs,
		},
	}

	cli := newFakeClient(&existingSecret, &generatedSecret)

	r := secretgenerator.SecretGeneratorReconciler{
		Client: cli,
	}

	_, err := r.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      existingSecret.Name,
			Namespace: existingSecret.Namespace,
		},
	})

	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Get(ctx, client.ObjectKeyFromObject(&generatedSecret), &generatedSecret)
	g.Expect(err).ShouldNot(HaveOccurred())

	// assuming an existing secret is left untouched
	g.Expect(generatedSecret.OwnerReferences).To(BeEmpty())
	g.Expect(generatedSecret.Labels).To(BeEmpty())
	g.Expect(generatedSecret.StringData).To(BeEmpty())
}

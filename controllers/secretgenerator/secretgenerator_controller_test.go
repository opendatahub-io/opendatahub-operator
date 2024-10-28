package secretgenerator_test

import (
	"context"
	"testing"

	"github.com/onsi/gomega/gstruct"
	oauthv1 "github.com/openshift/api/oauth/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
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
	utilruntime.Must(oauthv1.AddToScheme(scheme))

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

func TestSecretNotFound(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	secretName := "fooo"
	secretNs := "foooNs"

	cli := newFakeClient()

	r := secretgenerator.SecretGeneratorReconciler{
		Client: cli,
	}

	_, err := r.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      secretName,
			Namespace: secretNs,
		},
	})
	// secret not found, reconcile should end without error
	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestDeleteOAuthClientIfSecretNotFound(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	secretName := "fooo"
	secretNs := "foooNs"

	// secret expected to be deleted
	existingSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNs,
			Labels: map[string]string{
				"fooo": "bar",
			},
			Annotations: map[string]string{
				annotations.SecretNameAnnotation: "bar",
				annotations.SecretTypeAnnotation: "random",
			},
		},
	}

	// future left-over oauth client to be cleaned up during reconcile
	existingOauthClient := oauthv1.OAuthClient{
		TypeMeta: metav1.TypeMeta{
			Kind:       "OAuthClient",
			APIVersion: "oauth.openshift.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNs,
		},
		Secret:       secretName,
		RedirectURIs: []string{"https://foo.bar"},
		GrantMethod:  oauthv1.GrantHandlerAuto,
	}

	cli := newFakeClient(&existingSecret, &existingOauthClient)

	r := secretgenerator.SecretGeneratorReconciler{
		Client: cli,
	}

	// delete secret
	err := cli.Delete(ctx, &existingSecret)
	g.Expect(err).ShouldNot(HaveOccurred())

	// ensure the secret is deleted
	err = cli.Get(ctx, client.ObjectKeyFromObject(&existingSecret), &existingSecret)
	g.Expect(err).To(MatchError(k8serr.IsNotFound, "NotFound"))

	_, err = r.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      secretName,
			Namespace: secretNs,
		},
	})
	// secret not found, reconcile should clean-up left-over oauth client and end without error
	g.Expect(err).ShouldNot(HaveOccurred())

	// ensure the leftover OauthClient was deleted during reconcile
	foundOauthClient := oauthv1.OAuthClient{}
	err = cli.Get(ctx, client.ObjectKeyFromObject(&existingOauthClient), &foundOauthClient)
	g.Expect(err).To(MatchError(k8serr.IsNotFound, "NotFound"))
}

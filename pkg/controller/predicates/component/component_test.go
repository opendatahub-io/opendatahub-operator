package component_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"

	. "github.com/onsi/gomega"
)

func assertUpdateFuncChecksOldAndNewLabels(t *testing.T, pred predicate.Funcs, labelName, labelValue string) {
	t.Helper()
	t.Parallel()

	g := NewWithT(t)

	// new object has matching label
	g.Expect(pred.Update(event.UpdateEvent{
		ObjectOld: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
		},
		ObjectNew: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-ns",
				Labels: map[string]string{labelName: labelValue},
			},
		},
	})).To(BeTrue())

	// old object has matching label
	g.Expect(pred.Update(event.UpdateEvent{
		ObjectOld: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-ns",
				Labels: map[string]string{labelName: labelValue},
			},
		},
		ObjectNew: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
		},
	})).To(BeTrue())

	// both have matching label
	g.Expect(pred.Update(event.UpdateEvent{
		ObjectOld: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-ns",
				Labels: map[string]string{labelName: labelValue},
			},
		},
		ObjectNew: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-ns",
				Labels: map[string]string{labelName: labelValue},
			},
		},
	})).To(BeTrue())

	// neither has matching label
	g.Expect(pred.Update(event.UpdateEvent{
		ObjectOld: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
		},
		ObjectNew: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
		},
	})).To(BeFalse())
}

func TestForLabel(t *testing.T) {
	t.Parallel()

	const (
		labelName  = "app.opendatahub.io/part-of"
		labelValue = "test-component"
	)

	pred := component.ForLabel(labelName, labelValue)

	t.Run("CreateFunc always returns false", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(pred.Create(event.CreateEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-ns",
					Labels: map[string]string{labelName: labelValue},
				},
			},
		})).To(BeFalse())

		g.Expect(pred.Create(event.CreateEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
		})).To(BeFalse())
	})

	t.Run("DeleteFunc checks label", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(pred.Delete(event.DeleteEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-ns",
					Labels: map[string]string{labelName: labelValue},
				},
			},
		})).To(BeTrue())

		g.Expect(pred.Delete(event.DeleteEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
		})).To(BeFalse())

		g.Expect(pred.Delete(event.DeleteEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-ns",
					Labels: map[string]string{labelName: "wrong-value"},
				},
			},
		})).To(BeFalse())
	})

	t.Run("UpdateFunc checks old and new labels", func(t *testing.T) {
		assertUpdateFuncChecksOldAndNewLabels(t, pred, labelName, labelValue)
	})
}

func TestForLabelAllEvents(t *testing.T) {
	t.Parallel()

	const (
		labelName  = "app.opendatahub.io/part-of"
		labelValue = "test-component"
	)

	pred := component.ForLabelAllEvents(labelName, labelValue)

	t.Run("CreateFunc checks label", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(pred.Create(event.CreateEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-ns",
					Labels: map[string]string{labelName: labelValue},
				},
			},
		})).To(BeTrue())

		g.Expect(pred.Create(event.CreateEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
		})).To(BeFalse())

		g.Expect(pred.Create(event.CreateEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-ns",
					Labels: map[string]string{labelName: "wrong-value"},
				},
			},
		})).To(BeFalse())
	})

	t.Run("DeleteFunc checks label", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(pred.Delete(event.DeleteEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-ns",
					Labels: map[string]string{labelName: labelValue},
				},
			},
		})).To(BeTrue())

		g.Expect(pred.Delete(event.DeleteEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
		})).To(BeFalse())
	})

	t.Run("UpdateFunc checks both old and new labels", func(t *testing.T) {
		assertUpdateFuncChecksOldAndNewLabels(t, pred, labelName, labelValue)
	})
}

func TestForAnnotation(t *testing.T) {
	t.Parallel()

	const (
		annotationName  = "test-annotation"
		annotationValue = "test-value"
	)

	pred := component.ForAnnotation(annotationName, annotationValue)

	t.Run("CreateFunc always returns false", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(pred.Create(event.CreateEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ns",
					Annotations: map[string]string{annotationName: annotationValue},
				},
			},
		})).To(BeFalse())
	})

	t.Run("DeleteFunc checks annotation", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(pred.Delete(event.DeleteEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ns",
					Annotations: map[string]string{annotationName: annotationValue},
				},
			},
		})).To(BeTrue())

		g.Expect(pred.Delete(event.DeleteEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
		})).To(BeFalse())

		g.Expect(pred.Delete(event.DeleteEvent{
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ns",
					Annotations: map[string]string{annotationName: "wrong-value"},
				},
			},
		})).To(BeFalse())
	})

	t.Run("UpdateFunc checks old and new annotations", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		// new object has matching annotation
		g.Expect(pred.Update(event.UpdateEvent{
			ObjectOld: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
			},
			ObjectNew: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ns",
					Annotations: map[string]string{annotationName: annotationValue},
				},
			},
		})).To(BeTrue())

		// old object has matching annotation
		g.Expect(pred.Update(event.UpdateEvent{
			ObjectOld: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ns",
					Annotations: map[string]string{annotationName: annotationValue},
				},
			},
			ObjectNew: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
			},
		})).To(BeTrue())

		// neither has matching annotation
		g.Expect(pred.Update(event.UpdateEvent{
			ObjectOld: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
			},
			ObjectNew: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
			},
		})).To(BeFalse())
	})
}

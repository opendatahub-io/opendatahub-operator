//go:build nowebhook

package webhook

import ctrl "sigs.k8s.io/controller-runtime"

func Init(mgr ctrl.Manager) {}

{{/*
Expand the name of the chart.
*/}}
{{- define "workbenches-operator.name" -}}
workbenches-operator
{{- end }}

{{/*
Resource name prefix (matches config/default namePrefix).
*/}}
{{- define "workbenches-operator.namePrefix" -}}
{{- .Values.namePrefix | default "workbenches-operator-" -}}
{{- end }}

{{/*
Prefixed resource name: {{ namePrefix }}<suffix>
Usage: include "workbenches-operator.prefixed" (list . "controller-manager")
*/}}
{{- define "workbenches-operator.prefixed" -}}
{{- $root := index . 0 -}}
{{- $suffix := index . 1 -}}
{{- printf "%s%s" (include "workbenches-operator.namePrefix" $root) $suffix -}}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "workbenches-operator.serviceAccountName" -}}
{{- include "workbenches-operator.prefixed" (list . .Values.serviceAccount.name) -}}
{{- end }}

{{/*
Deployment name. Must match ModuleHandler ReleaseName (workbenches-operator) so
the platform injectModuleEnv action can find this Deployment by metadata.name.
*/}}
{{- define "workbenches-operator.deploymentName" -}}
{{- .Release.Name -}}
{{- end }}

{{/*
ClusterRole names.
*/}}
{{- define "workbenches-operator.managerClusterRoleName" -}}
{{- include "workbenches-operator.prefixed" (list . "manager-role") -}}
{{- end }}

{{- define "workbenches-operator.escalateClusterRoleName" -}}
{{- include "workbenches-operator.prefixed" (list . "manager-rbac-escalate-role") -}}
{{- end }}

{{/*
Leader election Role name (namespace-scoped).
*/}}
{{- define "workbenches-operator.leaderElectionRoleName" -}}
{{- include "workbenches-operator.prefixed" (list . "leader-election-role") -}}
{{- end }}

{{/*
Common labels applied to operator resources.
*/}}
{{- define "workbenches-operator.labels" -}}
app.kubernetes.io/name: {{ include "workbenches-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: controller
control-plane: controller-manager
{{- end }}

{{/*
Selector labels for the Deployment pod template.
*/}}
{{- define "workbenches-operator.selectorLabels" -}}
control-plane: controller-manager
app.kubernetes.io/name: {{ include "workbenches-operator.name" . }}
{{- end }}

{{/*
Operator container arguments.
*/}}
{{- define "workbenches-operator.managerArgs" -}}
- --health-probe-bind-address=:8081
- --manifests-base-path={{ .Values.manifests.basePath }}
{{- if .Values.leaderElection.enabled }}
- --leader-elect
{{- end }}
{{- if ne .Values.metrics.bindAddress "0" }}
- --metrics-bind-address={{ .Values.metrics.bindAddress }}
{{- end }}
{{- if .Values.metrics.secure }}
- --metrics-secure=true
{{- else }}
- --metrics-secure=false
{{- end }}
{{- if .Values.webhooks.enabled }}
- --enable-webhooks=true
{{- else }}
- --enable-webhooks=false
{{- end }}
{{- if .Values.devLogging }}
- --zap-devel=true
{{- end }}
{{- end }}

{{/*
Manager container image reference.

Resolution order:
  1. relatedImages[controllerImage.relatedImageEnv] — simulates injectModuleEnv /
     helm --set for local testing with RELATED_IMAGE_* values
  2. image.digest — digest-pinned product builds via Helm values
  3. params.workbenchesOperatorImage — default from params.env
  4. image.repository:image.tag — fallback
*/}}
{{- define "workbenches-operator.image" -}}
{{- $controllerEnv := .Values.controllerImage.relatedImageEnv -}}
{{- if and $controllerEnv (index .Values.relatedImages $controllerEnv) -}}
{{- index .Values.relatedImages $controllerEnv -}}
{{- else if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else if .Values.params.workbenchesOperatorImage -}}
{{- .Values.params.workbenchesOperatorImage -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository .Values.image.tag -}}
{{- end -}}
{{- end }}

{{/*
Webhook Service name.
*/}}
{{- define "workbenches-operator.webhookServiceName" -}}
{{- include "workbenches-operator.prefixed" (list . "webhook-service") -}}
{{- end }}

{{/*
Webhook cert Secret name. Uses webhooks.certSecret when set, otherwise derives
from namePrefix to match the kustomize convention.
*/}}
{{- define "workbenches-operator.webhookCertSecret" -}}
{{- if .Values.webhooks.certSecret -}}
{{- .Values.webhooks.certSecret -}}
{{- else -}}
{{- include "workbenches-operator.prefixed" (list . "controller-webhook-cert") -}}
{{- end -}}
{{- end }}

{{/*
cert-manager Certificate name.
*/}}
{{- define "workbenches-operator.webhookCertificateName" -}}
{{- include "workbenches-operator.prefixed" (list . "webhook-cert") -}}
{{- end }}

{{/*
cert-manager Issuer name. Uses webhooks.certManager.issuerRef.name when set,
otherwise derives from namePrefix.
*/}}
{{- define "workbenches-operator.webhookIssuerName" -}}
{{- if .Values.webhooks.certManager.issuerRef.name -}}
{{- .Values.webhooks.certManager.issuerRef.name -}}
{{- else -}}
{{- include "workbenches-operator.prefixed" (list . "selfsigned-issuer") -}}
{{- end -}}
{{- end }}

{{/*
MutatingWebhookConfiguration name.
*/}}
{{- define "workbenches-operator.mutatingWebhookName" -}}
{{- include "workbenches-operator.prefixed" (list . "mutating-webhook-configuration") -}}
{{- end }}

{{/*
Container environment variables. APPLICATIONS_NAMESPACE is appended last so
extraEnv cannot override it (Kubernetes uses the last duplicate env name).
*/}}
{{- define "workbenches-operator.managerEnv" -}}
{{- with .Values.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- range $name, $value := .Values.relatedImages }}
{{- if $value }}
- name: {{ $name }}
  value: {{ $value | quote }}
{{- end }}
{{- end }}
- name: APPLICATIONS_NAMESPACE
  value: {{ .Values.applicationsNamespace | quote }}
{{- end }}

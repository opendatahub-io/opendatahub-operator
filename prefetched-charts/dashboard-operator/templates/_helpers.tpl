{{/*
Expand the name of the chart.
*/}}
{{- define "dashboard.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "dashboard.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart and selector labels
*/}}
{{- define "dashboard.labels" -}}
helm.sh/chart: {{ include "dashboard.name" . }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: dashboard-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{- define "dashboard.selectorLabels" -}}
app.kubernetes.io/name: dashboard-operator
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "dashboard.namespace" -}}
{{- default .Release.Namespace .Values.namespace }}
{{- end }}

{{- define "dashboard.deploymentName" -}}
{{- printf "%sdashboard-operator" .Values.namePrefix }}
{{- end }}

{{- define "dashboard.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- printf "%s%s" .Values.namePrefix (.Values.serviceAccount.name | default "dashboard-operator") }}
{{- else }}
{{- required "serviceAccount.name must be set when serviceAccount.create=false" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "dashboard.clusterRoleName" -}}
{{- printf "%sdashboard-operator-role" .Values.namePrefix }}
{{- end }}

{{- define "dashboard.clusterRoleBindingName" -}}
{{- printf "%sdashboard-operator-rolebinding" .Values.namePrefix }}
{{- end }}

{{- define "dashboard.configMapName" -}}
{{- .Values.config.name }}
{{- end }}

{{- define "dashboard.image" -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) }}
{{- end }}

{{- define "dashboard.webhookServiceName" -}}
{{- printf "%sdashboard-operator-webhook" .Values.namePrefix }}
{{- end }}

{{- define "dashboard.webhookName" -}}
{{- printf "%sdashboard-operator-validating" .Values.namePrefix }}
{{- end }}

{{- define "dashboard.webhookIssuerName" -}}
{{- printf "%sdashboard-operator-selfsigned" .Values.namePrefix }}
{{- end }}

{{- define "dashboard.webhookCertName" -}}
{{- printf "%sdashboard-operator-webhook-cert" .Values.namePrefix }}
{{- end }}

{{- define "dashboard.webhookCertSecretName" -}}
{{- printf "%sdashboard-operator-webhook-tls" .Values.namePrefix }}
{{- end }}

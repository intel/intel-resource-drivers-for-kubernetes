{{/* Define common helpers */}}
{{- define "intel-gaudi-resource-driver.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/* Define the base name for the driver */}}
{{- define "intel-gaudi-resource-driver.baseName" -}}
intel-gaudi-resource-driver
{{- end }}

{{/* Specific helpers */}}
{{- define "intel-gaudi-resource-driver.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Create a default fully qualified app name */}}
{{- define "intel-gaudi-resource-driver.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else -}}
{{- printf "%s-%s" (include "intel-gaudi-resource-driver.baseName" .) .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end }}

{{- define "intel-gaudi-resource-driver.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride }}
{{- end }}

{{/* Labels for templates */}}
{{- define "intel-gaudi-resource-driver.labels" -}}
helm.sh/chart: {{ include "intel-gaudi-resource-driver.chart" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "intel-gaudi-resource-driver.clusterRoleName" -}}
{{- printf "%s-role" (include "intel-gaudi-resource-driver.baseName" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "intel-gaudi-resource-driver.clusterRoleBindingName" -}}
{{- printf "%s-rolebinding" (include "intel-gaudi-resource-driver.baseName" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "intel-gaudi-resource-driver.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default "intel-gaudi-sa" .Values.serviceAccount.name -}}
{{- end -}}
{{- end }}

{{/* Define full image name */}}
{{- define "intel-gaudi-resource-driver.fullimage" -}}
{{- printf "%s/%s:%s" .Values.image.repository .Values.image.name .Values.image.tag -}}
{{- end }}

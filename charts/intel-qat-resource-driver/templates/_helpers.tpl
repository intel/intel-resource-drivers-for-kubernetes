{{/* Define common helpers */}}
{{- define "intel-qat-resource-driver.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/* Define the base name for the driver */}}
{{- define "intel-qat-resource-driver.baseName" -}}
intel-qat-resource-driver
{{- end }}

{{- define "intel-qat-resource-driver.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "intel-qat-resource-driver.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else -}}
{{- printf "%s-%s" (include "intel-qat-resource-driver.baseName" .) .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end }}

{{/* Labels for templates */}}
{{- define "intel-qat-resource-driver.labels" -}}
helm.sh/chart: {{ include "intel-qat-resource-driver.chart" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "intel-qat-resource-driver.clusterRoleName" -}}
{{- printf "%s-role" (include "intel-qat-resource-driver.baseName" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "intel-qat-resource-driver.clusterRoleBindingName" -}}
{{- printf "%s-rolebinding" (include "intel-qat-resource-driver.baseName" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "intel-qat-resource-driver.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default "intel-qat-sa" .Values.serviceAccount.name -}}
{{- end -}}
{{- end }}

{{/* Define full image name */}}
{{- define "intel-qat-resource-driver.fullimage" -}}
{{- printf "%s/%s:%s" .Values.image.repository .Values.image.name .Values.image.tag -}}
{{- end }}

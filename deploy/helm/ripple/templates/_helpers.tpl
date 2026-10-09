{{/* Common template helpers. */}}

{{- define "ripple.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "ripple.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "ripple.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "ripple.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ripple.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "ripple.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "ripple.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "ripple.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "ripple.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "ripple.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}

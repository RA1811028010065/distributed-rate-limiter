{{- define "rate-limiter.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "rate-limiter.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "rate-limiter.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "rate-limiter.labels" -}}
app.kubernetes.io/name: {{ include "rate-limiter.name" . }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "rate-limiter.selectorLabels" -}}
app: {{ include "rate-limiter.fullname" . }}
{{- end -}}

{{- define "rate-limiter.natsName" -}}
{{- printf "%s-nats" (include "rate-limiter.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

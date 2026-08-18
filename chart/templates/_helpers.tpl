{{- define "guardrail.name" -}}
woop-pod-resource-guardrail
{{- end -}}

{{- define "guardrail.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "guardrail.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "guardrail.labels" -}}
app.kubernetes.io/name: {{ include "guardrail.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "guardrail.tlsSecret" -}}
{{- default (printf "%s-tls" (include "guardrail.fullname" .)) .Values.webhook.existingTLSSecret -}}
{{- end -}}

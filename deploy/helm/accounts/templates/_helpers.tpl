{{- define "accounts.name" -}}
accounts
{{- end -}}

{{- define "accounts.fullname" -}}
{{ .Release.Name }}-accounts
{{- end -}}

{{- define "accounts.labels" -}}
app.kubernetes.io/name: {{ include "accounts.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end -}}

{{- define "accounts.selectorLabels" -}}
app.kubernetes.io/name: {{ include "accounts.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "accounts.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{ include "accounts.fullname" . }}
{{- else -}}
default
{{- end -}}
{{- end -}}

type FieldErrorValue = string | string[]

interface APIErrorPayload {
  detail?: string
  message?: string
  fields?: Record<string, FieldErrorValue>
  errors?: Record<string, FieldErrorValue>
  metadata?: Record<string, unknown>
}

interface APIErrorLike {
  detail?: string
  message?: string
  metadata?: Record<string, unknown>
  response?: {
    data?: APIErrorPayload
  }
}

const firstFieldError = (value: unknown): string | null => {
  if (Array.isArray(value)) {
    const first = value[0]
    return typeof first === 'string' && first !== '' ? first : null
  }
  return typeof value === 'string' && value !== '' ? value : null
}

export const apiKeyGroupFieldError = (
  error: unknown,
  fields: readonly string[] = ['group_ids', 'group_id']
): string | null => {
  if (!error || typeof error !== 'object') return null

  const source = error as APIErrorLike
  const payload = source.response?.data
  for (const field of fields) {
    const direct =
      firstFieldError(payload?.fields?.[field]) ??
      firstFieldError(payload?.errors?.[field])
    if (direct) return direct
  }

  const metadata = payload?.metadata ?? source.metadata
  const metadataField = typeof metadata?.field === 'string' ? metadata.field : ''
  if (metadataField && fields.includes(metadataField)) {
    return payload?.detail ?? payload?.message ?? source.detail ?? source.message ?? null
  }

  return payload?.detail ?? payload?.message ?? null
}

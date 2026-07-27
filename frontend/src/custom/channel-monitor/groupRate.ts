export const GROUP_RATE_DISPLAY_TEMPLATE_PLACEHOLDER = '{rate}'
export const DEFAULT_GROUP_RATE_DISPLAY_TEMPLATE = '{rate}x'
export const GROUP_RATE_DISPLAY_TEMPLATE_MAX_LENGTH = 64

export function formatGroupRateMultiplier(rate: number): string {
  if (!Number.isFinite(rate)) return ''
  const normalized = Object.is(rate, -0) ? 0 : rate
  return Number(normalized.toFixed(4)).toString()
}

export function normalizeGroupRateDisplayTemplate(template?: string | null): string {
  return template?.trim() ?? ''
}

export function isValidGroupRateDisplayTemplate(template?: string | null): boolean {
  const normalized = normalizeGroupRateDisplayTemplate(template)
  if (!normalized) return true
  if ([...normalized].length > GROUP_RATE_DISPLAY_TEMPLATE_MAX_LENGTH) return false
  return normalized.split(GROUP_RATE_DISPLAY_TEMPLATE_PLACEHOLDER).length - 1 === 1
}

export function renderGroupRateDisplay(rate: number, template?: string | null): string {
  const formattedRate = formatGroupRateMultiplier(rate)
  if (!formattedRate || !isValidGroupRateDisplayTemplate(template)) return ''

  const normalizedTemplate =
    normalizeGroupRateDisplayTemplate(template) || DEFAULT_GROUP_RATE_DISPLAY_TEMPLATE
  return normalizedTemplate.replace(GROUP_RATE_DISPLAY_TEMPLATE_PLACEHOLDER, formattedRate)
}

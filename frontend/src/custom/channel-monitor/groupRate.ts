export const GROUP_RATE_DISPLAY_TEMPLATE_PLACEHOLDER = '{rate}'
export const DEFAULT_GROUP_RATE_DISPLAY_TEMPLATE = '{rate}x'
export const GROUP_RATE_DISPLAY_TEMPLATE_MAX_LENGTH = 64

export interface MonitorGroupRateFormState {
  override: number | '' | null
  displayTemplate: string
}

export interface MonitorGroupRateSource {
  group_rate_override?: number | null
  group_rate_display_template?: string | null
}

export function createEmptyMonitorGroupRateFormState(): MonitorGroupRateFormState {
  return { override: null, displayTemplate: '' }
}

export function monitorGroupRateFormStateFromSource(
  source: MonitorGroupRateSource,
): MonitorGroupRateFormState {
  return {
    override: source.group_rate_override ?? null,
    displayTemplate: source.group_rate_display_template || '',
  }
}

export function normalizedMonitorGroupRateOverride(
  state: MonitorGroupRateFormState,
): number | null {
  if (state.override === '' || state.override === null) return null
  return Number(state.override)
}

export function validateMonitorGroupRateForm(
  state: MonitorGroupRateFormState,
): 'admin.channelMonitor.groupRateOverrideInvalid'
  | 'admin.channelMonitor.groupRateDisplayTemplateInvalid'
  | null {
  const override = normalizedMonitorGroupRateOverride(state)
  if (override !== null && (!Number.isFinite(override) || override <= 0)) {
    return 'admin.channelMonitor.groupRateOverrideInvalid'
  }
  if (!isValidGroupRateDisplayTemplate(state.displayTemplate)) {
    return 'admin.channelMonitor.groupRateDisplayTemplateInvalid'
  }
  return null
}

export function buildMonitorGroupRateCreateFields(state: MonitorGroupRateFormState) {
  return {
    group_rate_override: normalizedMonitorGroupRateOverride(state),
    group_rate_display_template: state.displayTemplate.trim(),
  }
}

export function buildMonitorGroupRateUpdateFields(state: MonitorGroupRateFormState) {
  const override = normalizedMonitorGroupRateOverride(state)
  return {
    ...(override === null
      ? { clear_group_rate_override: true as const }
      : { group_rate_override: override }),
    group_rate_display_template: state.displayTemplate.trim(),
  }
}

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

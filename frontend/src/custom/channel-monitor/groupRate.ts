export function formatGroupRateMultiplier(rate: number): string {
  if (!Number.isFinite(rate)) return ''
  const normalized = Object.is(rate, -0) ? 0 : rate
  return Number(normalized.toFixed(4)).toString()
}

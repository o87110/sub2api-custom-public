interface DecimalFraction {
  numerator: bigint
  denominator: bigint
}

const ZERO_DECIMAL_PAYMENT_CURRENCIES = new Set([
  'BIF', 'CLP', 'DJF', 'GNF', 'JPY', 'KMF', 'KRW', 'MGA', 'PYG',
  'RWF', 'VND', 'VUV', 'XAF', 'XOF', 'XPF', 'ISK', 'UGX',
])
const THREE_DECIMAL_PAYMENT_CURRENCIES = new Set([
  'BHD', 'IQD', 'JOD', 'KWD', 'LYD', 'OMR', 'TND',
])

export function paymentCurrencyFractionDigits(currency: string): number {
  const normalized = String(currency || '').trim().toUpperCase()
  if (ZERO_DECIMAL_PAYMENT_CURRENCIES.has(normalized)) return 0
  if (THREE_DECIMAL_PAYMENT_CURRENCIES.has(normalized)) return 3
  return 2
}

function decimalFraction(value: number): DecimalFraction | undefined {
  if (!Number.isFinite(value) || value < 0) return undefined
  const match = String(value).toLowerCase().match(/^(\d+)(?:\.(\d+))?(?:e([+-]?\d+))?$/)
  if (!match) return undefined
  const fractionDigits = match[2] || ''
  const exponent = Number(match[3] || 0)
  let scale = fractionDigits.length - exponent
  let numerator = BigInt(`${match[1]}${fractionDigits}`)
  if (scale < 0) {
    numerator *= 10n ** BigInt(-scale)
    scale = 0
  }
  return {
    numerator,
    denominator: 10n ** BigInt(scale),
  }
}

export function isPaymentAmountRepresentable(value: number, currency: string): boolean {
  const amount = decimalFraction(value)
  if (!amount) return false
  const factor = 10n ** BigInt(paymentCurrencyFractionDigits(currency))
  return (amount.numerator * factor) % amount.denominator === 0n
}

export function roundPaymentAmount(value: number, currency: string): number {
  if (!Number.isFinite(value)) return 0
  const factor = 10 ** paymentCurrencyFractionDigits(currency)
  return Math.round(value * factor) / factor
}

export function multiplyAndRoundPaymentAmount(
  value: number,
  multiplier: number,
  currency: string,
): number {
  const amount = decimalFraction(value)
  const rate = decimalFraction(multiplier)
  if (!amount || !rate) return 0
  const factor = 10n ** BigInt(paymentCurrencyFractionDigits(currency))
  const numerator = amount.numerator * rate.numerator * factor
  const denominator = amount.denominator * rate.denominator
  let roundedMinor = numerator / denominator
  if ((numerator % denominator) * 2n >= denominator) {
    roundedMinor += 1n
  }
  if (roundedMinor > BigInt(Number.MAX_SAFE_INTEGER)) return 0
  return Number(roundedMinor) / Number(factor)
}

export function paymentFeeAmount(value: number, rate: number, currency: string): number {
  if (value <= 0 || rate <= 0) return 0
  const amount = decimalFraction(value)
  const feeRate = decimalFraction(rate)
  if (!amount || !feeRate) return 0
  const factor = 10n ** BigInt(paymentCurrencyFractionDigits(currency))
  const scaledAmount = amount.numerator * factor
  if (scaledAmount % amount.denominator !== 0n) return 0
  const amountMinor = scaledAmount / amount.denominator
  const numerator = amountMinor * feeRate.numerator
  const denominator = feeRate.denominator * 100n
  const feeMinor = (numerator + denominator - 1n) / denominator
  if (feeMinor > BigInt(Number.MAX_SAFE_INTEGER)) return 0
  return Number(feeMinor) / Number(factor)
}

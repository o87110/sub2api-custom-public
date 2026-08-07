import { describe, expect, it } from 'vitest'
import { baseCompile } from '@intlify/message-compiler'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

function renderMessage(message: string): string {
  const { code } = baseCompile(message, { mode: 'arrow' })
  const render = new Function(`return ${code}`)() as (context: {
    normalize: (values: unknown[]) => string
  }) => string
  return render({ normalize: values => values.join('') })
}

describe('channel monitor group rate messages', () => {
  it.each([
    [
      'zh',
      zh.admin.channelMonitor.groupRateDisplayTemplateInvalid,
      zh.admin.channelMonitor.form.groupRateDisplayTemplateHint,
      '倍率显示模板最多 64 个字符，且必须且只能包含一个 {rate}',
      '必须且只能包含一个 {rate}；留空时显示为 {rate}x。'
    ],
    [
      'en',
      en.admin.channelMonitor.groupRateDisplayTemplateInvalid,
      en.admin.channelMonitor.form.groupRateDisplayTemplateHint,
      'Rate display template must be at most 64 characters and contain exactly one {rate}',
      'Must contain exactly one {rate}; blank displays as {rate}x.'
    ]
  ] as const)('%s preserves literal rate placeholders', (_locale, invalidMessage, hintMessage, invalid, hint) => {
    expect(renderMessage(invalidMessage)).toBe(invalid)
    expect(renderMessage(hintMessage)).toBe(hint)
  })
})

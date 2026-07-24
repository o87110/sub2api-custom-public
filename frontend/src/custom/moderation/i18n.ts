export type CustomModerationTextKey =
  | 'autoBanNo'
  | 'notificationNotSent'
  | 'inputDetailContent'
  | 'inputDetailKeywordContext'
  | 'cyberPolicyOutOfScope'
  | 'violationNotCounted'

const messages: Record<'zh' | 'en', Record<CustomModerationTextKey, string>> = {
  zh: {
    autoBanNo: '自动封号：否',
    notificationNotSent: '通知状态：未发送',
    inputDetailContent: '输入摘要',
    inputDetailKeywordContext: '输入开头与命中上下文',
    cyberPolicyOutOfScope: 'Cyber Policy（审计范围外，仅留痕）',
    violationNotCounted: '未计入'
  },
  en: {
    autoBanNo: 'Auto ban: No',
    notificationNotSent: 'Notification: Not sent',
    inputDetailContent: 'Input Excerpt',
    inputDetailKeywordContext: 'Input Start and Matched Context',
    cyberPolicyOutOfScope: 'Cyber Policy (out of audit scope, log only)',
    violationNotCounted: 'Not counted'
  }
}

export function customModerationText(locale: string, key: CustomModerationTextKey): string {
  return messages[locale.toLowerCase().startsWith('zh') ? 'zh' : 'en'][key]
}

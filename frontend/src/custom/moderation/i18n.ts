export type CustomModerationTextKey =
  | 'autoBanNo'
  | 'notificationNotSent'
  | 'inputDetailContent'
  | 'inputDetailKeywordContext'
  | 'cyberPolicyOutOfScope'
  | 'violationNotCounted'
  | 'overallAuditScope'
  | 'overallAuditScopeHint'
  | 'apiAuditScope'
  | 'apiAuditScopeHint'
  | 'apiAuditAllInScope'
  | 'apiAuditSelectedInScope'
  | 'apiAuditSearchGroups'
  | 'apiAuditInactive'
  | 'apiAuditEmpty'
  | 'apiAuditAllSummary'
  | 'apiAuditSelectedSummary'

const messages: Record<'zh' | 'en', Record<CustomModerationTextKey, string>> = {
  zh: {
    autoBanNo: '自动封号：否',
    notificationNotSent: '通知状态：未发送',
    inputDetailContent: '输入摘要',
    inputDetailKeywordContext: '输入开头与命中上下文',
    cyberPolicyOutOfScope: 'Cyber Policy（审计范围外，仅留痕）',
    violationNotCounted: '未计入',
    overallAuditScope: '总审计范围',
    overallAuditScopeHint: '先确定关键词拦截和 API 审计共同适用的分组；API 可在下方继续缩小范围。',
    apiAuditScope: 'API 审计范围',
    apiAuditScopeHint: '仅从总审计范围内二次筛选调用上游审核 API 的分组；不会改变关键词拦截范围。',
    apiAuditAllInScope: '范围内全部分组',
    apiAuditSelectedInScope: '范围内指定分组',
    apiAuditSearchGroups: '搜索总审计范围内的分组',
    apiAuditInactive: '当前为「仅关键词」策略，API 审计范围已保留，切换到包含 API 的策略后生效。',
    apiAuditEmpty: 'API 审计指定范围至少需要选择一个分组。',
    apiAuditAllSummary: '总范围内全部分组',
    apiAuditSelectedSummary: '总范围内 {count} 个分组'
  },
  en: {
    autoBanNo: 'Auto ban: No',
    notificationNotSent: 'Notification: Not sent',
    inputDetailContent: 'Input Excerpt',
    inputDetailKeywordContext: 'Input Start and Matched Context',
    cyberPolicyOutOfScope: 'Cyber Policy (out of audit scope, log only)',
    violationNotCounted: 'Not counted',
    overallAuditScope: 'Overall audit scope',
    overallAuditScopeHint: 'Choose the shared group boundary for keyword blocking and API moderation; API coverage can be narrowed below.',
    apiAuditScope: 'API moderation scope',
    apiAuditScopeHint: 'Select which groups inside the overall scope call the upstream moderation API. Keyword coverage is unchanged.',
    apiAuditAllInScope: 'All groups in scope',
    apiAuditSelectedInScope: 'Selected groups in scope',
    apiAuditSearchGroups: 'Search groups in the overall audit scope',
    apiAuditInactive: 'The Keyword-only strategy is active. This API scope is retained and will apply after switching to a strategy that includes API moderation.',
    apiAuditEmpty: 'Select at least one group for API moderation.',
    apiAuditAllSummary: 'All groups in the overall scope',
    apiAuditSelectedSummary: '{count} groups in the overall scope'
  }
}

export function customModerationText(locale: string, key: CustomModerationTextKey): string {
  return messages[locale.toLowerCase().startsWith('zh') ? 'zh' : 'en'][key]
}

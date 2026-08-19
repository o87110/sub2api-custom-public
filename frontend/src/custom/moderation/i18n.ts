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
  | 'defaultBanThreshold'
  | 'defaultBanThresholdHint'
  | 'apiAuditBanEnabled'
  | 'apiAuditBanHint'
  | 'apiAuditBanThreshold'
  | 'apiAuditBanActiveHint'
  | 'apiAuditBanDisabledHint'
  | 'apiAuditBanMasterDisabledHint'
  | 'apiAuditBanInvalid'
  | 'apiAuditBanValidationFailed'
  | 'userBanThresholdTitle'
  | 'userBanThresholdHint'
  | 'userBanThresholdInactiveHint'
  | 'userBanThresholdSearchPlaceholder'
  | 'userBanThresholdSearchEmpty'
  | 'userBanThresholdSearchFailed'
  | 'userBanThresholdEmpty'
  | 'userBanThresholdField'
  | 'userBanThresholdInvalid'
  | 'userBanThresholdValidationFailed'
  | 'userBanThresholdDeleted'
  | 'userBanThresholdUnavailable'
  | 'userBanThresholdAdmin'
  | 'userBanThresholdRemove'
  | 'userBanThresholdDeletedRejected'
  | 'userBanThresholdAdminRejected'
  | 'userBanThresholdUserLoadFailed'
  | 'userBanThresholdUserFallback'

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
    apiAuditSelectedSummary: '总范围内 {count} 个分组',
    defaultBanThreshold: '默认封禁触发次数',
    defaultBanThresholdHint: '关键词、API 审计及未排除的 cyber_policy 共用此累计；用户专属阈值只覆盖这里。',
    apiAuditBanEnabled: '启用 API 审计专属封禁阈值',
    apiAuditBanHint: 'API 审计命中仍计入上方总累计，并额外进入专属累计；任一达到阈值即封号，因此该规则只能收紧封禁条件。',
    apiAuditBanThreshold: 'API 审计专属封禁次数',
    apiAuditBanActiveHint: 'API 专属累计达到该次数时执行自动封禁。',
    apiAuditBanDisabledHint: '关闭时保留当前阈值，但专属累计不触发封号。',
    apiAuditBanMasterDisabledHint: '自动封禁总开关已关闭；API 专属配置会保留，重新开启后生效。',
    apiAuditBanInvalid: '请输入 1–1000 的整数。',
    apiAuditBanValidationFailed: '请先修正 API 审计专属封禁次数。',
    userBanThresholdTitle: '用户专属封禁阈值',
    userBanThresholdHint: '指定用户命中时使用这里的绝对阈值，其他用户继续使用默认封禁触发次数。',
    userBanThresholdInactiveHint: '自动封禁当前已关闭；专属阈值会保留，并在重新开启后生效。',
    userBanThresholdSearchPlaceholder: '按邮箱、用户名或备注搜索用户',
    userBanThresholdSearchEmpty: '没有可添加的用户',
    userBanThresholdSearchFailed: '搜索用户失败，请稍后重试。',
    userBanThresholdEmpty: '尚未配置用户专属阈值',
    userBanThresholdField: '封禁触发次数',
    userBanThresholdInvalid: '请输入 1–1000 的整数。',
    userBanThresholdValidationFailed: '请先修正用户专属封禁阈值。',
    userBanThresholdDeleted: '已删除',
    userBanThresholdUnavailable: '用户不可用',
    userBanThresholdAdmin: '管理员不自动封禁',
    userBanThresholdRemove: '移除用户专属阈值',
    userBanThresholdDeletedRejected: '不能为已删除用户新增专属阈值。',
    userBanThresholdAdminRejected: '管理员账户不会被自动封禁，不能添加专属阈值。',
    userBanThresholdUserLoadFailed: '读取用户详情失败，请稍后重试。',
    userBanThresholdUserFallback: '用户 #{id}'
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
    apiAuditSelectedSummary: '{count} groups in the overall scope',
    defaultBanThreshold: 'Default ban trigger count',
    defaultBanThresholdHint: 'Keyword, API moderation, and non-excluded cyber_policy hits share this total. User overrides only replace this threshold.',
    apiAuditBanEnabled: 'Enable API moderation-specific ban threshold',
    apiAuditBanHint: 'API moderation hits still count toward the total above and also enter this dedicated count. Either threshold can ban, so this rule can only make enforcement stricter.',
    apiAuditBanThreshold: 'API moderation ban trigger count',
    apiAuditBanActiveHint: 'Automatic banning runs when the dedicated API moderation count reaches this value.',
    apiAuditBanDisabledHint: 'The value is retained while disabled, but the dedicated count cannot trigger a ban.',
    apiAuditBanMasterDisabledHint: 'Automatic banning is off. This configuration is retained and will apply when automatic banning is enabled again.',
    apiAuditBanInvalid: 'Enter an integer from 1 to 1000.',
    apiAuditBanValidationFailed: 'Fix the API moderation ban trigger count before saving.',
    userBanThresholdTitle: 'User-specific ban thresholds',
    userBanThresholdHint: 'Matched users use the absolute threshold below; everyone else keeps the default ban trigger count.',
    userBanThresholdInactiveHint: 'Automatic banning is off. These thresholds are retained and will apply when it is enabled again.',
    userBanThresholdSearchPlaceholder: 'Search by email, username, or note',
    userBanThresholdSearchEmpty: 'No users available to add',
    userBanThresholdSearchFailed: 'Unable to search users. Try again later.',
    userBanThresholdEmpty: 'No user-specific thresholds configured',
    userBanThresholdField: 'Ban trigger count',
    userBanThresholdInvalid: 'Enter an integer from 1 to 1000.',
    userBanThresholdValidationFailed: 'Fix the user-specific ban thresholds before saving.',
    userBanThresholdDeleted: 'Deleted',
    userBanThresholdUnavailable: 'User unavailable',
    userBanThresholdAdmin: 'Admins are never auto-banned',
    userBanThresholdRemove: 'Remove user-specific threshold',
    userBanThresholdDeletedRejected: 'A threshold cannot be added for a deleted user.',
    userBanThresholdAdminRejected: 'Admin accounts are never auto-banned and cannot have a custom threshold.',
    userBanThresholdUserLoadFailed: 'Unable to load user details. Try again later.',
    userBanThresholdUserFallback: 'User #{id}'
  }
}

export function customModerationText(
  locale: string,
  key: CustomModerationTextKey,
  params?: Record<string, string | number>
): string {
  const message = messages[locale.toLowerCase().startsWith('zh') ? 'zh' : 'en'][key]
  if (!params) return message
  return message.replace(/\{(\w+)\}/g, (_, token: string) => String(params[token] ?? `{${token}}`))
}

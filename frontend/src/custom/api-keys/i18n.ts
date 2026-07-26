export type ApiKeyBulkTextKey =
  | 'selected'
  | 'clear'
  | 'targetGroup'
  | 'selectGroup'
  | 'searchGroup'
  | 'noGroups'
  | 'applyGroup'
  | 'enable'
  | 'disable'
  | 'delete'
  | 'processing'
  | 'groupAction'
  | 'enableAction'
  | 'disableAction'
  | 'deleteAction'
  | 'noChanges'
  | 'success'
  | 'partial'
  | 'failed'
  | 'disableConfirmTitle'
  | 'disableConfirmMessage'
  | 'disableConfirm'
  | 'enableConfirmTitle'
  | 'enableConfirmMessage'
  | 'enableConfirm'
  | 'deleteConfirmTitle'
  | 'deleteConfirmMessage'
  | 'deleteConfirm'
  | 'selectionLabel'

type TextParams = Record<string, string | number>
type TextFactory = (params: TextParams) => string

const zhMessages: Record<ApiKeyBulkTextKey, TextFactory> = {
  selected: ({ count }) => `已选择 ${count} 个密钥`,
  clear: () => '清空选择',
  targetGroup: () => '目标分组',
  selectGroup: () => '选择目标分组',
  searchGroup: () => '搜索分组',
  noGroups: () => '没有可用分组',
  applyGroup: () => '应用分组',
  enable: () => '批量启用',
  disable: () => '批量禁用',
  delete: () => '批量删除',
  processing: () => '处理中…',
  groupAction: () => '切换分组',
  enableAction: () => '启用',
  disableAction: () => '禁用',
  deleteAction: () => '删除',
  noChanges: ({ action, skipped }) => `${action}无需更新，已跳过 ${skipped} 项。`,
  success: ({ action, success, skipped }) =>
    `${action}完成：成功 ${success} 项，跳过 ${skipped} 项。`,
  partial: ({ action, success, skipped, failed }) =>
    `${action}部分完成：成功 ${success} 项，跳过 ${skipped} 项，失败 ${failed} 项；失败项已保留选中，可重试。`,
  failed: ({ action, failed }) =>
    `${action}失败，共 ${failed} 项；失败项已保留选中，可重试。`,
  disableConfirmTitle: () => '确认批量禁用',
  disableConfirmMessage: ({ count, skipped }) =>
    `将禁用 ${count} 个 API 密钥${Number(skipped) > 0 ? `，另有 ${skipped} 个已禁用项会跳过` : ''}。禁用后这些密钥的调用将立即失败。`,
  disableConfirm: ({ count }) => `确认禁用 ${count} 个`,
  enableConfirmTitle: () => '确认批量启用',
  enableConfirmMessage: ({ count }) =>
    `将启用 ${count} 个 API 密钥。启用后这些密钥将立即恢复调用能力。`,
  enableConfirm: ({ count }) => `确认启用 ${count} 个`,
  deleteConfirmTitle: () => '确认批量删除',
  deleteConfirmMessage: ({ count }) =>
    `将永久删除选中的 ${count} 个 API 密钥。删除后密钥立即失效，且无法恢复。`,
  deleteConfirm: ({ count }) => `确认删除 ${count} 个`,
  selectionLabel: ({ name }) => `选择 API 密钥 ${name}`
}

const enMessages: Record<ApiKeyBulkTextKey, TextFactory> = {
  selected: ({ count }) => `${count} API keys selected`,
  clear: () => 'Clear selection',
  targetGroup: () => 'Target group',
  selectGroup: () => 'Select target group',
  searchGroup: () => 'Search groups',
  noGroups: () => 'No groups available',
  applyGroup: () => 'Apply group',
  enable: () => 'Enable selected',
  disable: () => 'Disable selected',
  delete: () => 'Delete selected',
  processing: () => 'Processing…',
  groupAction: () => 'Change group',
  enableAction: () => 'Enable',
  disableAction: () => 'Disable',
  deleteAction: () => 'Delete',
  noChanges: ({ action, skipped }) => `${action} requires no update; ${skipped} skipped.`,
  success: ({ action, success, skipped }) =>
    `${action} completed: ${success} succeeded, ${skipped} skipped.`,
  partial: ({ action, success, skipped, failed }) =>
    `${action} partially completed: ${success} succeeded, ${skipped} skipped, ${failed} failed. Failed items remain selected for retry.`,
  failed: ({ action, failed }) =>
    `${action} failed for ${failed} items. Failed items remain selected for retry.`,
  disableConfirmTitle: () => 'Confirm bulk disable',
  disableConfirmMessage: ({ count, skipped }) =>
    `Disable ${count} API keys${Number(skipped) > 0 ? ` and skip ${skipped} already disabled` : ''}? Their requests will fail immediately.`,
  disableConfirm: ({ count }) => `Disable ${count}`,
  enableConfirmTitle: () => 'Confirm bulk enable',
  enableConfirmMessage: ({ count }) =>
    `Enable ${count} API keys? They will regain API access immediately.`,
  enableConfirm: ({ count }) => `Enable ${count}`,
  deleteConfirmTitle: () => 'Confirm bulk delete',
  deleteConfirmMessage: ({ count }) =>
    `Permanently delete the selected ${count} API keys? They will stop working immediately and cannot be recovered.`,
  deleteConfirm: ({ count }) => `Delete ${count}`,
  selectionLabel: ({ name }) => `Select API key ${name}`
}

const messages: Record<'zh' | 'en', Record<ApiKeyBulkTextKey, TextFactory>> = {
  zh: zhMessages,
  en: enMessages
}

export function customApiKeyBulkText(
  locale: string,
  key: ApiKeyBulkTextKey,
  params: TextParams = {}
): string {
  return messages[locale.toLowerCase().startsWith('zh') ? 'zh' : 'en'][key](params)
}

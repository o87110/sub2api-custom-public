export type ApiKeyGroupPriorityTextKey =
  | 'label'
  | 'help'
  | 'addPlaceholder'
  | 'add'
  | 'empty'
  | 'priority'
  | 'moveUp'
  | 'moveDown'
  | 'remove'
  | 'save'
  | 'cancel'
  | 'maxReached'
  | 'samePlatform'
  | 'added'
  | 'moved'
  | 'removed'
  | 'summary'
  | 'edit'

type Params = Record<string, string | number>
type Messages = Record<ApiKeyGroupPriorityTextKey, (params: Params) => string>

const zh: Messages = {
  label: () => '分组优先级',
  help: () => '新会话优先使用列表顶部的分组；会话降级后会保持当前分组以复用缓存。',
  addPlaceholder: () => '选择要添加的分组',
  add: () => '添加分组',
  empty: () => '尚未选择分组',
  priority: ({ priority }) => `优先级 ${priority}`,
  moveUp: ({ name }) => `上移分组 ${name}`,
  moveDown: ({ name }) => `下移分组 ${name}`,
  remove: ({ name }) => `移除分组 ${name}`,
  save: () => '保存优先级',
  cancel: () => '取消修改',
  maxReached: ({ max }) => `最多可选择 ${max} 个分组`,
  samePlatform: ({ platform }) => `后续分组必须与首个分组使用同一平台（${platform}）`,
  added: ({ name, priority }) => `已添加 ${name}，优先级 ${priority}`,
  moved: ({ name, priority }) => `已将 ${name} 移到优先级 ${priority}`,
  removed: ({ name }) => `已移除 ${name}`,
  summary: ({ name, extra }) => `${name}${Number(extra) > 0 ? ` + ${extra}` : ''}`,
  edit: () => '编辑分组优先级'
}

const en: Messages = {
  label: () => 'Group priority',
  help: () => 'New sessions start at the top. After failover, an active session stays on its current group to preserve cache.',
  addPlaceholder: () => 'Select a group to add',
  add: () => 'Add group',
  empty: () => 'No groups selected',
  priority: ({ priority }) => `Priority ${priority}`,
  moveUp: ({ name }) => `Move ${name} up`,
  moveDown: ({ name }) => `Move ${name} down`,
  remove: ({ name }) => `Remove ${name}`,
  save: () => 'Save priority',
  cancel: () => 'Cancel changes',
  maxReached: ({ max }) => `You can select up to ${max} groups`,
  samePlatform: ({ platform }) => `Additional groups must use the first group's platform (${platform})`,
  added: ({ name, priority }) => `${name} added at priority ${priority}`,
  moved: ({ name, priority }) => `${name} moved to priority ${priority}`,
  removed: ({ name }) => `${name} removed`,
  summary: ({ name, extra }) => `${name}${Number(extra) > 0 ? ` + ${extra}` : ''}`,
  edit: () => 'Edit group priority'
}

export function apiKeyGroupPriorityText(
  locale: string,
  key: ApiKeyGroupPriorityTextKey,
  params: Params = {}
): string {
  return (locale.toLowerCase().startsWith('zh') ? zh : en)[key](params)
}

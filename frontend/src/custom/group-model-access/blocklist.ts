export interface GroupModelBlocklistItem {
  id: string
  blocked: boolean
}

export interface GroupModelBlocklistState {
  savedBlockedModels: string[]
  items: GroupModelBlocklistItem[]
}

export const normalizeBlockedModels = (models: string[]): string[] => {
  const seen = new Set<string>()
  const normalized: string[] = []
  for (const raw of models) {
    const model = raw.trim()
    if (!model || seen.has(model)) {
      continue
    }
    seen.add(model)
    normalized.push(model)
  }
  return normalized
}

export const includeSavedBlockedModels = (
  modelIDs: string[],
  savedBlockedModels: string[],
): string[] => normalizeBlockedModels([...modelIDs, ...savedBlockedModels])

export const blockedModelsForCandidates = (
  state: GroupModelBlocklistState,
  hasExistingItems: boolean,
): Set<string> => new Set(
  hasExistingItems
    ? state.items.filter(item => item.blocked).map(item => item.id)
    : state.savedBlockedModels,
)

export const toggleModelsBlocklistItem = (
  state: GroupModelBlocklistState,
  modelID: string,
) => {
  const item = state.items.find(item => item.id === modelID)
  if (item) {
    item.blocked = !item.blocked
  }
}

export const blockAllModelsListItems = (state: GroupModelBlocklistState) => {
  state.items.forEach(item => {
    item.blocked = true
  })
}

export const invertModelsBlocklistSelection = (state: GroupModelBlocklistState) => {
  state.items.forEach(item => {
    item.blocked = !item.blocked
  })
}

export const buildBlockedModelsPayload = (
  state: GroupModelBlocklistState,
): string[] => state.items.length > 0
  ? state.items.filter(item => item.blocked).map(item => item.id)
  : [...state.savedBlockedModels]

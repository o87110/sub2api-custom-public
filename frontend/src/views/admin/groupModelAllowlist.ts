import {
  blockedModelsForCandidates,
  blockAllModelsListItems,
  buildBlockedModelsPayload,
  type GroupModelBlocklistItem,
  type GroupModelBlocklistState,
  includeSavedBlockedModels,
  invertModelsBlocklistSelection,
  normalizeBlockedModels,
  toggleModelsBlocklistItem,
} from "@/custom/group-model-access/blocklist"

export {
  blockAllModelsListItems,
  invertModelsBlocklistSelection,
  toggleModelsBlocklistItem,
}

export interface ModelsListConfig {
  enabled: boolean
  models: string[]
  blocked_models: string[]
}

export interface ModelsListItem extends GroupModelBlocklistItem {
  selected: boolean
}

export interface ModelsListState extends GroupModelBlocklistState {
  enabled: boolean
  savedModels: string[]
  items: ModelsListItem[]
}

export const createModelsListState = (
  config?: Partial<ModelsListConfig> | null,
): ModelsListState => ({
  enabled: config?.enabled ?? false,
  savedModels: normalizeModels(config?.models ?? []),
  savedBlockedModels: normalizeBlockedModels(config?.blocked_models ?? []),
  items: [],
})

export const hydrateModelsListState = (
  config: Partial<ModelsListConfig> | null | undefined,
  candidates: string[],
): ModelsListState => {
  const state = createModelsListState(config)
  setModelsListCandidates(state, candidates)
  return state
}

export const setModelsListCandidates = (
  state: ModelsListState,
  candidates: string[],
) => {
  const normalizedCandidates = normalizeModels(candidates)
  const currentSelected = new Set(
    state.items.filter(item => item.selected).map(item => item.id),
  )
  const currentKnown = new Set(state.items.map(item => item.id))
  const savedSelected = new Set(state.savedModels)
  const hasExistingItems = state.items.length > 0
  const blockedModels = blockedModelsForCandidates(state, hasExistingItems)
  const selectionOrder = includeSavedBlockedModels(normalizeModels([
    ...state.items.map(item => item.id),
    ...state.savedModels,
    ...normalizedCandidates,
  ]), state.savedBlockedModels)

  state.items = selectionOrder.map(id => {
    const selected = hasExistingItems
      ? currentSelected.has(id)
      : state.savedModels.length > 0
        ? savedSelected.has(id)
        : normalizedCandidates.includes(id)

    return {
      id,
      selected: selected && (currentKnown.has(id) || savedSelected.has(id) || state.savedModels.length === 0),
      blocked: blockedModels.has(id),
    }
  })
}

export const toggleModelsListItem = (state: ModelsListState, modelID: string) => {
  const item = state.items.find(item => item.id === modelID)
  if (item) {
    item.selected = !item.selected
  }
}

export const selectAllModelsListItems = (state: ModelsListState) => {
  state.items.forEach(item => {
    item.selected = true
  })
}

export const invertModelsListSelection = (state: ModelsListState) => {
  state.items.forEach(item => {
    item.selected = !item.selected
  })
}

export const moveModelsListItem = (
  state: ModelsListState,
  fromIndex: number,
  toIndex: number,
) => {
  if (
    fromIndex === toIndex ||
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= state.items.length ||
    toIndex >= state.items.length
  ) {
    return
  }
  const [item] = state.items.splice(fromIndex, 1)
  state.items.splice(toIndex, 0, item)
}

export const buildModelsListConfig = (state: ModelsListState): ModelsListConfig => ({
  enabled: state.enabled,
  models: state.items.length > 0
    ? state.items.filter(item => item.selected).map(item => item.id)
    : [...state.savedModels],
  blocked_models: buildBlockedModelsPayload(state),
})

const normalizeModels = (models: string[]): string[] => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of models) {
    const model = raw.trim()
    if (!model || seen.has(model)) {
      continue
    }
    seen.add(model)
    out.push(model)
  }
  return out
}

// Official model allowlist API retained alongside the custom blocklist state.
export interface ModelAllowlistConfig { enabled: boolean; models: string[] }
export interface ModelAllowlistItem { id: string; selected: boolean }
export interface ModelAllowlistState { enabled: boolean; savedModels: string[]; items: ModelAllowlistItem[] }
export type ModelAllowlistAddError = 'empty' | 'invalid_wildcard' | 'duplicate'

export const createModelAllowlistState = (config?: Partial<ModelAllowlistConfig> | null): ModelAllowlistState => ({
  enabled: config?.enabled ?? false,
  savedModels: normalizeModels(config?.models ?? []),
  items: [],
})

export const hydrateModelAllowlistState = (
  config: Partial<ModelAllowlistConfig> | null | undefined,
  candidates: string[],
): ModelAllowlistState => {
  const state = createModelAllowlistState(config)
  setModelAllowlistCandidates(state, candidates)
  return state
}

export const setModelAllowlistCandidates = (state: ModelAllowlistState, candidates: string[]) => {
  const normalizedCandidates = normalizeModels(candidates)
  const currentSelected = new Set(state.items.filter(item => item.selected).map(item => item.id))
  const savedSelected = new Set(state.savedModels)
  const hasExistingItems = state.items.length > 0
  const selectionOrder = normalizeModels([...state.items.map(item => item.id), ...state.savedModels, ...normalizedCandidates])
  state.items = selectionOrder.map(id => ({
    id,
    selected: hasExistingItems
      ? currentSelected.has(id)
      : state.savedModels.length > 0 ? savedSelected.has(id) : normalizedCandidates.includes(id),
  }))
}

export const toggleModelAllowlistItem = (state: ModelAllowlistState, modelID: string) => {
  const item = state.items.find(item => item.id === modelID)
  if (item) item.selected = !item.selected
}

export const selectAllModelAllowlistItems = (state: ModelAllowlistState) => {
  state.items.forEach(item => { item.selected = true })
}

export const invertModelAllowlistSelection = (state: ModelAllowlistState) => {
  state.items.forEach(item => { item.selected = !item.selected })
}

export const moveModelAllowlistItem = (state: ModelAllowlistState, fromIndex: number, toIndex: number) => {
  if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || fromIndex >= state.items.length || toIndex >= state.items.length) return
  const [item] = state.items.splice(fromIndex, 1)
  state.items.splice(toIndex, 0, item)
}

export const buildModelAllowlistConfig = (state: ModelAllowlistState): ModelAllowlistConfig => ({
  enabled: state.enabled,
  models: state.items.length > 0 ? state.items.filter(item => item.selected).map(item => item.id) : [...state.savedModels],
})

export const addCustomModelAllowlistItem = (state: ModelAllowlistState, raw: string): ModelAllowlistAddError | null => {
  const entry = raw.trim()
  if (!entry) return 'empty'
  if (entry.slice(0, -1).includes('*')) return 'invalid_wildcard'
  if (state.items.some(item => item.id.toLowerCase() === entry.toLowerCase()) || state.savedModels.some(model => model.toLowerCase() === entry.toLowerCase())) return 'duplicate'
  state.items.push({ id: entry, selected: true })
  return null
}

export const selectedModelAllowlistCount = (state: ModelAllowlistState) => state.items.filter(item => item.selected).length

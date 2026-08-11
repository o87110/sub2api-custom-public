import { describe, expect, it } from "vitest"

import {
  blockAllModelsListItems,
  buildBlockedModelsPayload,
  includeSavedBlockedModels,
  invertModelsBlocklistSelection,
  normalizeBlockedModels,
  toggleModelsBlocklistItem,
} from "./blocklist"

describe("group model blocklist", () => {
  it("defaults to an empty exact-match blocklist and normalizes configured IDs", () => {
    expect(normalizeBlockedModels([])).toEqual([])
    expect(normalizeBlockedModels([" gpt-5.4-mini ", "", "gpt-5.4-mini", "gpt-5.6-luna"])).toEqual([
      "gpt-5.4-mini",
      "gpt-5.6-luna",
    ])
  })

  it("retains saved entries that disappeared from candidates", () => {
    expect(includeSavedBlockedModels(
      ["gpt-5.6-sol", "gpt-5.6-luna"],
      ["gpt-5.4-mini"],
    )).toEqual(["gpt-5.6-sol", "gpt-5.6-luna", "gpt-5.4-mini"])
  })

  it("toggles, blocks all and inverts independently from display state", () => {
    const state = {
      savedBlockedModels: [],
      items: [
        { id: "gpt-5.6-sol", selected: false, blocked: false },
        { id: "gpt-5.6-luna", selected: true, blocked: false },
      ],
    }

    toggleModelsBlocklistItem(state, "gpt-5.6-luna")
    expect(buildBlockedModelsPayload(state)).toEqual(["gpt-5.6-luna"])
    blockAllModelsListItems(state)
    invertModelsBlocklistSelection(state)

    expect(buildBlockedModelsPayload(state)).toEqual([])
    expect(state.items.map(item => item.selected)).toEqual([false, true])
  })
})

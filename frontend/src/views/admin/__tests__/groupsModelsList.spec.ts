import { describe, expect, it } from "vitest";

import {
  blockAllModelsListItems,
  buildModelsListConfig,
  createModelsListState,
  hydrateModelsListState,
  invertModelsBlocklistSelection,
  invertModelsListSelection,
  moveModelsListItem,
  selectAllModelsListItems,
  setModelsListCandidates,
  toggleModelsListItem,
  toggleModelsBlocklistItem,
} from "../groupsModelsList";

describe("groupsModelsList", () => {
  it("selects all default candidates for a new disabled config", () => {
    const state = createModelsListState();

    setModelsListCandidates(state, ["gpt-5.5", "gpt-5.4"]);

    expect(state.enabled).toBe(false);
    expect(state.items).toEqual([
      { id: "gpt-5.5", selected: true, blocked: false },
      { id: "gpt-5.4", selected: true, blocked: false },
    ]);
  });

  it("keeps saved selections and marks new candidates as unselected when editing", () => {
    const state = createModelsListState({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4"],
    });

    setModelsListCandidates(state, ["gpt-5.4", "legacy-gpt", "gpt-5.5"]);

    expect(state.enabled).toBe(true);
    expect(state.items).toEqual([
      { id: "gpt-5.5", selected: true, blocked: false },
      { id: "gpt-5.4", selected: true, blocked: false },
      { id: "legacy-gpt", selected: false, blocked: false },
    ]);
  });

  it("preserves explicitly unselected saved candidates when candidates refresh", () => {
    const state = createModelsListState({
      enabled: true,
      models: ["gpt-5.5"],
    });

    setModelsListCandidates(state, ["gpt-5.5", "gpt-5.4"]);

    expect(state.items).toEqual([
      { id: "gpt-5.5", selected: true, blocked: false },
      { id: "gpt-5.4", selected: false, blocked: false },
    ]);
  });

  it("builds config with selected models in current display order", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4", "legacy-gpt"],
    }, ["gpt-5.5", "gpt-5.4", "legacy-gpt"]);

    toggleModelsListItem(state, "legacy-gpt");
    moveModelsListItem(state, 1, 0);

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["gpt-5.4", "gpt-5.5"],
      blocked_models: [],
    });
  });

  it("keeps selected models in payload even when disabled so reopening can restore choices", () => {
    const state = hydrateModelsListState({
      enabled: false,
      models: ["gpt-5.5"],
      blocked_models: [],
    }, ["gpt-5.5", "gpt-5.4"]);

    expect(buildModelsListConfig(state)).toEqual({
      enabled: false,
      models: ["gpt-5.5"],
      blocked_models: [],
    });
  });

  it("preserves saved models when candidates have not loaded yet", () => {
    const state = createModelsListState({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4"],
      blocked_models: [],
    });

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4"],
      blocked_models: [],
    });
  });

  it("selects all candidate models from the toolbar action", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["gpt-5.5"],
    }, ["gpt-5.5", "gpt-5.4", "gpt-5.4-mini"]);

    selectAllModelsListItems(state);

    expect(state.items).toEqual([
      { id: "gpt-5.5", selected: true, blocked: false },
      { id: "gpt-5.4", selected: true, blocked: false },
      { id: "gpt-5.4-mini", selected: true, blocked: false },
    ]);
  });

  it("inverts selected models from the toolbar action", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["gpt-5.5"],
    }, ["gpt-5.5", "gpt-5.4", "gpt-5.4-mini"]);

    invertModelsListSelection(state);

    expect(state.items).toEqual([
      { id: "gpt-5.5", selected: false, blocked: false },
      { id: "gpt-5.4", selected: true, blocked: false },
      { id: "gpt-5.4-mini", selected: true, blocked: false },
    ]);
  });

  it("keeps blocked models that disappeared from candidates and builds an independent payload", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["gpt-5.6-sol"],
      blocked_models: ["gpt-5.4-mini"],
    }, ["gpt-5.6-sol", "gpt-5.6-luna"]);

    toggleModelsBlocklistItem(state, "gpt-5.6-luna");
    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["gpt-5.6-sol"],
      blocked_models: ["gpt-5.6-luna", "gpt-5.4-mini"],
    });
  });

  it("supports block-all and invert without changing the display selection", () => {
    const state = hydrateModelsListState(null, ["gpt-5.6-sol", "gpt-5.6-luna"]);
    blockAllModelsListItems(state);
    invertModelsBlocklistSelection(state);

    expect(state.items.every(item => item.selected)).toBe(true);
    expect(state.items.every(item => !item.blocked)).toBe(true);
  });
});

package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/custom/groupmodelaccess"
)

func normalizeGroupModelsListConfig(cfg GroupModelsListConfig) GroupModelsListConfig {
	out := GroupModelsListConfig{
		Enabled:       cfg.Enabled,
		BlockedModels: groupmodelaccess.Normalize(cfg.BlockedModels),
	}
	if len(cfg.Models) == 0 {
		return out
	}

	seen := make(map[string]struct{}, len(cfg.Models))
	out.Models = make([]string, 0, len(cfg.Models))
	for _, model := range cfg.Models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out.Models = append(out.Models, model)
	}
	if len(out.Models) == 0 {
		out.Models = nil
	}
	return out
}

func (g *Group) CustomModelsListEnabled() bool {
	return g != nil && g.ModelsListConfig.Enabled && len(g.ModelsListConfig.Models) > 0
}

func (g *Group) BlocksModel(model string) bool {
	return g != nil && groupmodelaccess.Blocks(g.ModelsListConfig.BlockedModels, model)
}

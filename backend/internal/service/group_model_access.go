package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/custom/groupmodelaccess"
	"github.com/gin-gonic/gin"
)

func WithGroupModelAccess(ctx context.Context, group *Group) context.Context {
	if group == nil {
		return ctx
	}
	return groupmodelaccess.WithAdditionalModels(ctx, group.ModelsListConfig.BlockedModels)
}

func WithCurrentGroupModelAccess(ctx context.Context, group *Group) context.Context {
	if group == nil {
		return groupmodelaccess.WithPolicy(ctx, groupmodelaccess.Policy{})
	}
	return groupmodelaccess.WithPolicy(ctx, groupmodelaccess.NewPolicy(group.ModelsListConfig.BlockedModels))
}

func CheckGroupModelAccess(ctx context.Context, model string) error {
	return groupmodelaccess.Check(ctx, model)
}

// BindGroupModelAccessRequest replaces the request policy with the latest
// persisted group snapshot and rechecks the client-facing model. It is used by
// detached work immediately before it starts forwarding upstream.
func BindGroupModelAccessRequest(req *http.Request, group *Group) (string, error) {
	var blockedModels []string
	if group != nil {
		blockedModels = group.ModelsListConfig.BlockedModels
	}
	model, blocked, err := groupmodelaccess.BindAndInspectRequest(req, blockedModels)
	if err != nil || !blocked {
		return model, err
	}
	return model, &groupmodelaccess.BlockedModelError{Model: model}
}

func GroupModelBlockedModel(err error) string {
	var blockedErr *groupmodelaccess.BlockedModelError
	if errors.As(err, &blockedErr) && blockedErr != nil {
		return strings.TrimSpace(blockedErr.Model)
	}
	return ""
}

func CheckGatewayAccountModelAccess(ctx context.Context, account *Account, requestedModel string) error {
	return groupmodelaccess.Check(ctx, resolveGatewayAccountModelForAccess(ctx, account, requestedModel))
}

func CheckOpenAIAccountModelAccess(ctx context.Context, account *Account, requestedModel string, requireCompact bool) error {
	return groupmodelaccess.Check(ctx, resolveOpenAIAccountModelForAccess(ctx, account, requestedModel, requireCompact))
}

func enforceResolvedModelAccess(ctx context.Context, c *gin.Context, model string) error {
	err := groupmodelaccess.Check(ctx, model)
	if err == nil {
		return nil
	}
	if c != nil {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		groupmodelaccess.WriteBlockedResponse(c, model)
	}
	return err
}

func IsGroupModelBlockedError(err error) bool {
	return errors.Is(err, groupmodelaccess.ErrModelBlocked)
}

func attachSelectionGroupModelAccess(ctx context.Context, result *AccountSelectionResult) *AccountSelectionResult {
	if result != nil {
		result.modelAccessPolicy = groupmodelaccess.FromContext(ctx)
	}
	return result
}

// ContextWithSelectionGroupModelAccess replays the scheduler's union policy
// after fallback-group resolution so the final network guard cannot lose it.
func ContextWithSelectionGroupModelAccess(ctx context.Context, result *AccountSelectionResult) context.Context {
	if result == nil {
		return ctx
	}
	return groupmodelaccess.WithPolicy(ctx, groupmodelaccess.FromContext(ctx).Union(result.modelAccessPolicy))
}

// FilterCodexModelsManifest applies the display allow-list and the final-model
// policy while preserving unknown manifest fields and model entries.
func FilterCodexModelsManifest(
	ctx context.Context,
	account *Account,
	body []byte,
	displayModels []string,
	displayEnabled bool,
) ([]byte, bool, error) {
	policyEnabled := !groupmodelaccess.FromContext(ctx).Empty()
	if !policyEnabled && !displayEnabled {
		return body, false, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false, err
	}
	var models []json.RawMessage
	if err := json.Unmarshal(envelope["models"], &models); err != nil {
		return nil, false, err
	}
	allowed := make(map[string]struct{}, len(displayModels))
	for _, model := range displayModels {
		if model = strings.TrimSpace(model); model != "" {
			allowed[model] = struct{}{}
		}
	}
	filtered := make([]json.RawMessage, 0, len(models))
	changed := false
	for _, raw := range models {
		var item struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(raw, &item); err != nil || strings.TrimSpace(item.Slug) == "" {
			filtered = append(filtered, raw)
			continue
		}
		slug := strings.TrimSpace(item.Slug)
		if displayEnabled {
			if _, ok := allowed[slug]; !ok {
				changed = true
				continue
			}
		}
		if policyEnabled && modelAccessBlocksOpenAIAccount(ctx, account, slug, false) {
			changed = true
			continue
		}
		filtered = append(filtered, raw)
	}
	if !changed {
		return body, false, nil
	}
	modelsJSON, err := json.Marshal(filtered)
	if err != nil {
		return nil, false, err
	}
	envelope["models"] = modelsJSON
	filteredBody, err := json.Marshal(envelope)
	return filteredBody, true, err
}

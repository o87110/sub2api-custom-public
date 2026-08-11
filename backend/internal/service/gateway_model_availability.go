package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/custom/groupmodelaccess"
)

// FilterModelsByGroupAccess applies the request policy to public model IDs.
// A mapped public model is removed only when the persistent account pool has
// no remaining path whose final upstream model is allowed.
func (s *GatewayService) FilterModelsByGroupAccess(
	ctx context.Context,
	groupID *int64,
	platform string,
	models []string,
) []string {
	policy := groupmodelaccess.FromContext(ctx)
	if policy.Empty() || len(models) == 0 {
		return models
	}
	var group *Group
	if groupID != nil {
		group, _ = s.resolveGroupByID(ctx, *groupID)
	}
	out := make([]string, 0, len(models))
	for _, rawModel := range models {
		model := strings.TrimSpace(rawModel)
		if model == "" || policy.Blocks(model) {
			continue
		}

		modelCtx := ctx
		routingModel := model
		targetPlatform := platform
		if groupID != nil {
			if group != nil && group.Platform == PlatformComposite {
				if decision, ok, resolveErr := s.resolveCompositeRouteDecision(ctx, group, model, CompositeRouteEndpointAny); resolveErr == nil && ok {
					targetPlatform = decision.TargetPlatform
					routingModel = decision.UpstreamModel
					modelCtx = groupmodelaccess.WithRequestModel(modelCtx, routingModel)
				}
			}
			if mapping, _ := s.ResolveChannelMappingAndRestrict(modelCtx, groupID, routingModel); mapping.Mapped {
				modelCtx = groupmodelaccess.WithRequestModel(modelCtx, mapping.MappedModel)
			}
		}
		if group != nil && group.AllowMessagesDispatch {
			if fallbackModel := group.ResolveMessagesDispatchModel(model); fallbackModel != "" {
				modelCtx = groupmodelaccess.WithFallbackModel(modelCtx, fallbackModel)
			}
		}

		diagnosis := s.DiagnoseModelAvailabilityForPlatform(modelCtx, groupID, routingModel, targetPlatform)
		if diagnosis.HasAccountsInPool && !diagnosis.HasModelSupport {
			continue
		}
		out = append(out, model)
	}
	return out
}

func (s *GatewayService) FilterGeminiModelsResponse(
	ctx context.Context,
	groupID *int64,
	platform string,
	body []byte,
	displayModels []string,
	displayEnabled bool,
) ([]byte, bool, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false, err
	}
	var models []json.RawMessage
	if err := json.Unmarshal(envelope["models"], &models); err != nil {
		return nil, false, err
	}
	ids := make([]string, 0, len(models))
	for _, raw := range models {
		var item struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &item) == nil {
			ids = append(ids, strings.TrimPrefix(strings.TrimSpace(item.Name), "models/"))
		}
	}
	allowedByPolicy := s.FilterModelsByGroupAccess(ctx, groupID, platform, ids)
	allowed := make(map[string]struct{}, len(allowedByPolicy))
	for _, model := range allowedByPolicy {
		allowed[strings.TrimPrefix(strings.TrimSpace(model), "models/")] = struct{}{}
	}
	displayAllowed := make(map[string]struct{}, len(displayModels))
	for _, model := range displayModels {
		displayAllowed[strings.TrimPrefix(strings.TrimSpace(model), "models/")] = struct{}{}
	}

	filtered := make([]json.RawMessage, 0, len(models))
	changed := false
	for _, raw := range models {
		var item struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &item); err != nil || strings.TrimSpace(item.Name) == "" {
			filtered = append(filtered, raw)
			continue
		}
		id := strings.TrimPrefix(strings.TrimSpace(item.Name), "models/")
		_, policyOK := allowed[id]
		_, displayOK := displayAllowed[id]
		if !policyOK || (displayEnabled && !displayOK) {
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

// ModelAvailabilityDiagnosis describes whether the requested model can be
// served by any persistently eligible account in the group (active with its
// schedulable setting enabled), ignoring transient state such as rate limits,
// overload, temporary unschedulability, and runtime blocks. Handlers use this
// on the "no available accounts" error path to distinguish 404
// model_not_found from 503 service_unavailable.
type ModelAvailabilityDiagnosis struct {
	// HasAccountsInPool is true if the group has at least one persistently
	// eligible account on the queried platform (or, for Anthropic/Gemini, on
	// the platform plus mixed-scheduled Antigravity accounts).
	HasAccountsInPool bool
	// HasModelSupport is true if at least one account's model mapping admits
	// the requested model.
	HasModelSupport bool
	// PolicyBlocked is true when at least one persistent account path supports
	// the requested model before applying the group blocklist, but every such
	// path is removed by that local policy.
	PolicyBlocked bool
}

// ModelAvailabilityDiagnoser is implemented by gateway services that can
// report whether the requested model is configured to be served by any
// account. Both *GatewayService and *OpenAIGatewayService implement this so
// handlers in either package can share a single classifier.
type ModelAvailabilityDiagnoser interface {
	DiagnoseModelAvailabilityForPlatform(
		ctx context.Context,
		groupID *int64,
		requestedModel string,
		platform string,
	) ModelAvailabilityDiagnosis
}

// DiagnoseModelAvailabilityForPlatform inspects accounts enabled for scheduling
// by persistent configuration and returns whether the requested model is
// configured to be served by any of them. The dedicated repository query
// bypasses scheduler snapshots and deliberately ignores transient rate-limit,
// overload, temporary-unschedulable, expiry, quota, and runtime-block state.
//
// Safe to call on the error path: returns {true,true} on any internal failure
// or when the inputs preclude meaningful diagnosis (empty model, etc.), so
// callers stay on the 503 fallback branch.
func (s *GatewayService) DiagnoseModelAvailabilityForPlatform(
	ctx context.Context,
	groupID *int64,
	requestedModel string,
	platform string,
) ModelAvailabilityDiagnosis {
	if s == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		// No model specified — cannot decide model_not_found. Caller falls back to 503.
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	if strings.TrimSpace(platform) == "" {
		// Without a platform we cannot scope the lookup; bail out to the
		// 503 branch rather than make an unscoped scan.
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	if s.accountRepo == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	useMixed := platform == PlatformAnthropic || platform == PlatformGemini
	platforms := []string{platform}
	if useMixed {
		platforms = append(platforms, PlatformAntigravity)
	}

	queryGroupID := groupID
	includeGrouped := false
	if useMixed {
		// Preserve the generic scheduler's scope rules: an explicit group wins
		// for mixed scheduling, while group-less simple mode scans all accounts.
		if groupID == nil && s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
			includeGrouped = true
		}
	} else if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		queryGroupID = nil
		includeGrouped = true
	}

	accounts, err := s.accountRepo.ListModelAvailabilityCandidates(ctx, queryGroupID, platforms, includeGrouped)
	if err != nil {
		// Conservative fallback: pretend everything is fine so the caller
		// returns 503 (we don't want to flip to 404 just because a lookup
		// hiccup'd).
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	diag := ModelAvailabilityDiagnosis{}
	ctxWithoutPolicy := groupmodelaccess.WithPolicy(ctx, groupmodelaccess.Policy{})
	for i := range accounts {
		if useMixed && accounts[i].Platform == PlatformAntigravity && !accounts[i].IsMixedSchedulingEnabled() {
			continue
		}
		diag.HasAccountsInPool = true
		if s.isModelSupportedByAccountWithContext(ctx, &accounts[i], requestedModel) {
			diag.HasModelSupport = true
			diag.PolicyBlocked = false
			return diag
		}
		if modelAccessBlocksGatewayAccount(ctx, &accounts[i], requestedModel) &&
			s.isModelSupportedByAccountWithContext(ctxWithoutPolicy, &accounts[i], requestedModel) {
			diag.PolicyBlocked = true
		}
	}
	return diag
}

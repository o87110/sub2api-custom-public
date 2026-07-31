package groupaccess

import (
	"context"
	"errors"
	"fmt"
)

var ErrCircuitOpen = errors.New("minimum-balance circuit breaker is open")

type DependencyErrorKind string

const (
	DependencyUserMissing DependencyErrorKind = "user_missing"
	DependencyGroupLoader DependencyErrorKind = "group_loader"
	DependencyGroupLoad   DependencyErrorKind = "group_load"
	DependencyGroupCycle  DependencyErrorKind = "group_cycle"
	DependencyBalanceLoad DependencyErrorKind = "balance_load"
)

// DependencyError identifies infrastructure failures that the official
// billing bridge must translate to its stable billing-unavailable error.
type DependencyError struct {
	Kind DependencyErrorKind
	Err  error
}

func (e *DependencyError) Error() string {
	if e == nil || e.Err == nil {
		return "minimum-balance dependency failed"
	}
	return e.Err.Error()
}

func (e *DependencyError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// GroupSnapshot contains only the group fields used by the custom eligibility
// coordinator. Official service entities are projected into this type by the
// thin billing bridge.
type GroupSnapshot struct {
	ID              int64
	Name            string
	MinimumBalance  float64
	ClaudeCodeOnly  bool
	FallbackGroupID *int64
}

type EligibilityRequest struct {
	UserID           int64
	Group            *GroupSnapshot
	ForcePlatform    bool
	ClaudeCodeClient bool
}

type GroupLoader interface {
	LoadMinimumBalanceGroup(ctx context.Context, groupID int64) (*GroupSnapshot, error)
}

type BalanceLoader interface {
	LoadCurrentBalance(ctx context.Context, userID int64) (float64, error)
}

type CircuitBreaker interface {
	Allow() bool
	OnSuccess()
	OnFailure(error)
}

type EligibilityChecker struct {
	groups  GroupLoader
	balance BalanceLoader
	breaker CircuitBreaker
}

func NewEligibilityChecker(groups GroupLoader, balance BalanceLoader, breaker CircuitBreaker) *EligibilityChecker {
	return &EligibilityChecker{groups: groups, balance: balance, breaker: breaker}
}

// Check owns the complete minimum-balance qualification sequence: resolve the
// applicable fallback chain, consult the circuit breaker, load a fresh balance,
// and apply every active threshold.
func (c *EligibilityChecker) Check(ctx context.Context, req EligibilityRequest) (bool, error) {
	groups, err := c.groupsForRequest(ctx, req)
	if err != nil {
		return false, err
	}
	if len(groups) == 0 {
		return false, nil
	}
	if c.breaker != nil && !c.breaker.Allow() {
		return true, ErrCircuitOpen
	}
	if req.UserID <= 0 {
		return true, &DependencyError{Kind: DependencyUserMissing, Err: errors.New("group minimum-balance check requires a user")}
	}
	if c.balance == nil {
		return true, &DependencyError{Kind: DependencyBalanceLoad, Err: errors.New("minimum-balance balance loader is required")}
	}

	balance, err := c.balance.LoadCurrentBalance(ctx, req.UserID)
	if err != nil {
		if c.breaker != nil {
			c.breaker.OnFailure(err)
		}
		return true, &DependencyError{Kind: DependencyBalanceLoad, Err: err}
	}
	if c.breaker != nil {
		c.breaker.OnSuccess()
	}
	for _, group := range groups {
		if err := CheckMinimumBalance(group.ID, group.Name, balance, group.MinimumBalance); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (c *EligibilityChecker) groupsForRequest(ctx context.Context, req EligibilityRequest) ([]GroupSnapshot, error) {
	groups := make([]GroupSnapshot, 0, 2)
	if req.Group == nil {
		return groups, nil
	}
	if req.Group.MinimumBalance > 0 {
		groups = append(groups, *req.Group)
	}
	if req.ForcePlatform || !req.Group.ClaudeCodeOnly || req.ClaudeCodeClient || req.Group.FallbackGroupID == nil {
		return groups, nil
	}
	if c.groups == nil {
		return nil, &DependencyError{Kind: DependencyGroupLoader, Err: errors.New("group loader is required to resolve minimum-balance fallback")}
	}

	currentID := *req.Group.FallbackGroupID
	visited := map[int64]struct{}{req.Group.ID: {}}
	for {
		if _, seen := visited[currentID]; seen {
			return nil, &DependencyError{Kind: DependencyGroupCycle, Err: errors.New("fallback group cycle detected")}
		}
		visited[currentID] = struct{}{}

		fallback, err := c.groups.LoadMinimumBalanceGroup(ctx, currentID)
		if err != nil {
			return nil, &DependencyError{Kind: DependencyGroupLoad, Err: fmt.Errorf("resolve fallback group %d: %w", currentID, err)}
		}
		if fallback == nil {
			return nil, &DependencyError{Kind: DependencyGroupLoad, Err: fmt.Errorf("resolve fallback group %d: group is nil", currentID)}
		}
		if !fallback.ClaudeCodeOnly || req.ClaudeCodeClient {
			if fallback.MinimumBalance > 0 {
				groups = append(groups, *fallback)
			}
			return groups, nil
		}
		if fallback.FallbackGroupID == nil {
			return groups, nil
		}
		currentID = *fallback.FallbackGroupID
	}
}

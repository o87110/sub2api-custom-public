package groupaccess

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type eligibilityGroupLoaderStub struct {
	groups map[int64]*GroupSnapshot
	err    error
}

func (s eligibilityGroupLoaderStub) LoadMinimumBalanceGroup(_ context.Context, id int64) (*GroupSnapshot, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.groups[id], nil
}

type eligibilityBalanceLoaderStub struct {
	balance float64
	err     error
	calls   int
}

func (s *eligibilityBalanceLoaderStub) LoadCurrentBalance(context.Context, int64) (float64, error) {
	s.calls++
	return s.balance, s.err
}

type eligibilityBreakerStub struct {
	allow    bool
	success  int
	failures int
}

func (s *eligibilityBreakerStub) Allow() bool     { return s.allow }
func (s *eligibilityBreakerStub) OnSuccess()      { s.success++ }
func (s *eligibilityBreakerStub) OnFailure(error) { s.failures++ }

func TestEligibilityCheckerResolvesFallbackAndUsesFreshBalance(t *testing.T) {
	fallbackID := int64(2)
	balance := &eligibilityBalanceLoaderStub{balance: 100.01}
	breaker := &eligibilityBreakerStub{allow: true}
	checker := NewEligibilityChecker(eligibilityGroupLoaderStub{groups: map[int64]*GroupSnapshot{
		2: {ID: 2, Name: "fallback", MinimumBalance: 100},
	}}, balance, breaker)

	applied, err := checker.Check(context.Background(), EligibilityRequest{
		UserID: 7,
		Group: &GroupSnapshot{
			ID:              1,
			ClaudeCodeOnly:  true,
			FallbackGroupID: &fallbackID,
		},
	})
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, 1, balance.calls)
	require.Equal(t, 1, breaker.success)
}

func TestEligibilityCheckerSkipsDisabledAndForcedFallback(t *testing.T) {
	balance := &eligibilityBalanceLoaderStub{balance: 0}
	checker := NewEligibilityChecker(nil, balance, nil)
	applied, err := checker.Check(context.Background(), EligibilityRequest{Group: &GroupSnapshot{ID: 1}})
	require.NoError(t, err)
	require.False(t, applied)

	fallbackID := int64(2)
	applied, err = checker.Check(context.Background(), EligibilityRequest{
		Group:         &GroupSnapshot{ID: 1, ClaudeCodeOnly: true, FallbackGroupID: &fallbackID},
		ForcePlatform: true,
	})
	require.NoError(t, err)
	require.False(t, applied)
	require.Zero(t, balance.calls)
}

func TestEligibilityCheckerDetectsFallbackCycle(t *testing.T) {
	firstID := int64(2)
	backID := int64(1)
	checker := NewEligibilityChecker(eligibilityGroupLoaderStub{groups: map[int64]*GroupSnapshot{
		2: {ID: 2, ClaudeCodeOnly: true, FallbackGroupID: &backID},
	}}, &eligibilityBalanceLoaderStub{}, nil)

	_, err := checker.Check(context.Background(), EligibilityRequest{
		Group: &GroupSnapshot{ID: 1, ClaudeCodeOnly: true, FallbackGroupID: &firstID},
	})
	var dependencyErr *DependencyError
	require.ErrorAs(t, err, &dependencyErr)
	require.Equal(t, DependencyGroupCycle, dependencyErr.Kind)
}

func TestEligibilityCheckerReportsBalanceFailureAndCircuitState(t *testing.T) {
	loadErr := errors.New("database unavailable")
	balance := &eligibilityBalanceLoaderStub{err: loadErr}
	breaker := &eligibilityBreakerStub{allow: true}
	checker := NewEligibilityChecker(nil, balance, breaker)

	_, err := checker.Check(context.Background(), EligibilityRequest{
		UserID: 9,
		Group:  &GroupSnapshot{ID: 1, Name: "paid", MinimumBalance: 1},
	})
	var dependencyErr *DependencyError
	require.ErrorAs(t, err, &dependencyErr)
	require.Equal(t, DependencyBalanceLoad, dependencyErr.Kind)
	require.ErrorIs(t, err, loadErr)
	require.Equal(t, 1, breaker.failures)

	breaker.allow = false
	_, err = checker.Check(context.Background(), EligibilityRequest{
		UserID: 9,
		Group:  &GroupSnapshot{ID: 1, Name: "paid", MinimumBalance: 1},
	})
	require.ErrorIs(t, err, ErrCircuitOpen)
}

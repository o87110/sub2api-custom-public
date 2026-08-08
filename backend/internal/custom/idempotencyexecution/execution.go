package idempotencyexecution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid idempotency execution")

// Execution identifies the exact outer operation currently allowed to execute
// side effects. Nested transactional idempotency uses OperationKeyHash and the
// same lifetime as the outer record.
type Execution struct {
	Scope              string
	ActorScope         string
	IdempotencyKeyHash string
	OperationKeyHash   string
	ClaimedAt          time.Time
	ExpiresAt          time.Time
}

type contextKey struct{}

// New derives an item-level identity from the outer scope, actor scope, and
// the hash of the normalized Idempotency-Key.
func New(scope, actorScope, idempotencyKeyHash string, claimedAt, expiresAt time.Time) (Execution, error) {
	scope = strings.TrimSpace(scope)
	idempotencyKeyHash = strings.TrimSpace(idempotencyKeyHash)
	if scope == "" || idempotencyKeyHash == "" || claimedAt.IsZero() || !expiresAt.After(claimedAt) {
		return Execution{}, ErrInvalid
	}
	if actorScope == "" {
		actorScope = "anonymous"
	}
	return Execution{
		Scope:              scope,
		ActorScope:         actorScope,
		IdempotencyKeyHash: idempotencyKeyHash,
		OperationKeyHash:   hash(scope + "\n" + actorScope + "\n" + idempotencyKeyHash),
		ClaimedAt:          claimedAt,
		ExpiresAt:          expiresAt,
	}, nil
}

func WithContext(ctx context.Context, execution Execution) context.Context {
	return context.WithValue(ctx, contextKey{}, execution)
}

func FromContext(ctx context.Context) (Execution, bool) {
	if ctx == nil {
		return Execution{}, false
	}
	execution, ok := ctx.Value(contextKey{}).(Execution)
	return execution, ok
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

package service

import "context"

type multiGroupRoutingContextKey struct{}

// APIKeyGroupBinding is the internal CAS value for a sticky group assignment.
// GroupID is the routed group; Version prevents an older concurrent request
// from deleting or overwriting a binding refreshed by a newer request.
type APIKeyGroupBinding struct {
	GroupID int64
	Version string
}

// WithMultiGroupRouting 禁用单分组配置中的隐式 fallback。
// 多分组请求只能按 API Key 自身有序候选列表切换。
func WithMultiGroupRouting(ctx context.Context) context.Context {
	return context.WithValue(ctx, multiGroupRoutingContextKey{}, true)
}

func IsMultiGroupRouting(ctx context.Context) bool {
	enabled, _ := ctx.Value(multiGroupRoutingContextKey{}).(bool)
	return enabled
}

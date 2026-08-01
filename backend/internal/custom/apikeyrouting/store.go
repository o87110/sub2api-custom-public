package apikeyrouting

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const BindingTTL = time.Hour

var fallbackBindingVersion atomic.Uint64

func newVersionedBinding(groupID int64) service.APIKeyGroupBinding {
	if groupID <= 0 {
		return service.APIKeyGroupBinding{}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err == nil {
		return service.APIKeyGroupBinding{GroupID: groupID, Version: hex.EncodeToString(random[:])}
	}
	return service.APIKeyGroupBinding{
		GroupID: groupID,
		Version: strconv.FormatInt(time.Now().UnixNano(), 36) + "-" +
			strconv.FormatUint(fallbackBindingVersion.Add(1), 36),
	}
}

// BindingStore 使用比较更新避免并发请求覆盖或清除另一请求刚建立的绑定。
type BindingStore interface {
	LoadGroupBinding(ctx context.Context, apiKeyID int64, protocol, sessionHash string) (service.APIKeyGroupBinding, error)
	CompareAndSetGroupBinding(ctx context.Context, apiKeyID int64, protocol, sessionHash string, oldBinding, newBinding service.APIKeyGroupBinding, ttl time.Duration) (bool, error)
	CompareAndDeleteGroupBinding(ctx context.Context, apiKeyID int64, protocol, sessionHash string, oldBinding service.APIKeyGroupBinding) (bool, error)
}

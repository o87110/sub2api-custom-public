package apikeyrouting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const apiKeyGroupSessionPrefix = "api_key_group_session:"

var compareAndSetGroupBindingScript = redis.NewScript(`
	local current = redis.call('GET', KEYS[1])
	local expected = ARGV[1]
	if expected == '' then
		if current ~= false then
			return 0
		end
	elseif current == false or current ~= expected then
		return 0
	end
	redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
	return 1
`)

var compareAndDeleteGroupBindingScript = redis.NewScript(`
	local current = redis.call('GET', KEYS[1])
	if current == false or current ~= ARGV[1] then
		return 0
	end
	redis.call('DEL', KEYS[1])
	return 1
`)

func LoadRedisGroupBinding(
	ctx context.Context,
	client *redis.Client,
	apiKeyID int64,
	protocol string,
	sessionHash string,
) (service.APIKeyGroupBinding, error) {
	if apiKeyID <= 0 || protocol == "" || sessionHash == "" {
		return service.APIKeyGroupBinding{}, nil
	}
	value, err := client.Get(ctx, buildGroupBindingKey(apiKeyID, protocol, sessionHash)).Result()
	if err == redis.Nil {
		return service.APIKeyGroupBinding{}, nil
	}
	if err != nil {
		return service.APIKeyGroupBinding{}, err
	}
	return decodeRedisGroupBinding(value)
}

func CompareAndSetRedisGroupBinding(
	ctx context.Context,
	client *redis.Client,
	apiKeyID int64,
	protocol string,
	sessionHash string,
	oldBinding service.APIKeyGroupBinding,
	newBinding service.APIKeyGroupBinding,
	ttl time.Duration,
) (bool, error) {
	if apiKeyID <= 0 || protocol == "" || sessionHash == "" || newBinding.GroupID <= 0 || newBinding.Version == "" || ttl <= 0 {
		return false, nil
	}
	result, err := compareAndSetGroupBindingScript.Run(
		ctx,
		client,
		[]string{buildGroupBindingKey(apiKeyID, protocol, sessionHash)},
		encodeRedisGroupBinding(oldBinding),
		encodeRedisGroupBinding(newBinding),
		int64(ttl/time.Second),
	).Int()
	return result == 1, err
}

func CompareAndDeleteRedisGroupBinding(
	ctx context.Context,
	client *redis.Client,
	apiKeyID int64,
	protocol string,
	sessionHash string,
	oldBinding service.APIKeyGroupBinding,
) (bool, error) {
	if apiKeyID <= 0 || protocol == "" || sessionHash == "" || oldBinding.GroupID <= 0 {
		return false, nil
	}
	result, err := compareAndDeleteGroupBindingScript.Run(
		ctx,
		client,
		[]string{buildGroupBindingKey(apiKeyID, protocol, sessionHash)},
		encodeRedisGroupBinding(oldBinding),
	).Int()
	return result == 1, err
}

func encodeRedisGroupBinding(binding service.APIKeyGroupBinding) string {
	if binding.GroupID <= 0 {
		return ""
	}
	groupID := strconv.FormatInt(binding.GroupID, 10)
	if binding.Version == "" {
		return groupID
	}
	return groupID + ":" + binding.Version
}

func decodeRedisGroupBinding(value string) (service.APIKeyGroupBinding, error) {
	groupValue, version, _ := strings.Cut(strings.TrimSpace(value), ":")
	groupID, err := strconv.ParseInt(groupValue, 10, 64)
	if err != nil || groupID <= 0 {
		return service.APIKeyGroupBinding{}, fmt.Errorf("invalid API key group binding")
	}
	return service.APIKeyGroupBinding{GroupID: groupID, Version: version}, nil
}

func buildGroupBindingKey(apiKeyID int64, protocol string, sessionHash string) string {
	sum := sha256.Sum256([]byte(protocol + "\x00" + sessionHash))
	return fmt.Sprintf("%s%d:%s", apiKeyGroupSessionPrefix, apiKeyID, hex.EncodeToString(sum[:]))
}

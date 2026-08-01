package service

import (
	"context"
	"time"
)

func (s *OpenAIGatewayService) LoadGroupBinding(ctx context.Context, apiKeyID int64, protocol, sessionHash string) (APIKeyGroupBinding, error) {
	cache, ok := s.cache.(apiKeyGroupSessionCache)
	if !ok || sessionHash == "" {
		return APIKeyGroupBinding{}, nil
	}
	return cache.LoadGroupBinding(ctx, apiKeyID, protocol, sessionHash)
}

func (s *OpenAIGatewayService) CompareAndSetGroupBinding(
	ctx context.Context,
	apiKeyID int64,
	protocol, sessionHash string,
	oldBinding, newBinding APIKeyGroupBinding,
	ttl time.Duration,
) (bool, error) {
	cache, ok := s.cache.(apiKeyGroupSessionCache)
	if !ok || sessionHash == "" {
		return false, nil
	}
	return cache.CompareAndSetGroupBinding(ctx, apiKeyID, protocol, sessionHash, oldBinding, newBinding, ttl)
}

func (s *OpenAIGatewayService) CompareAndDeleteGroupBinding(
	ctx context.Context,
	apiKeyID int64,
	protocol, sessionHash string,
	oldBinding APIKeyGroupBinding,
) (bool, error) {
	cache, ok := s.cache.(apiKeyGroupSessionCache)
	if !ok || sessionHash == "" {
		return false, nil
	}
	return cache.CompareAndDeleteGroupBinding(ctx, apiKeyID, protocol, sessionHash, oldBinding)
}

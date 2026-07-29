package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *apiKeyRepository) GetGroupByIDForMinimumBalance(ctx context.Context, groupID int64) (*service.Group, error) {
	return (&groupRepository{client: r.client}).GetByIDLite(ctx, groupID)
}

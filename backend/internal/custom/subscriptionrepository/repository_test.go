package subscriptionrepository

import "github.com/Wei-Shaw/sub2api/internal/service"

// The transaction semantics of Repository rely on PostgreSQL row locks and
// are covered by user_subscription_repo_integration_test.go. SQLite rejects
// SELECT FOR UPDATE, so this unit file only guards the Custom port wiring.
var (
	_ service.UserSubscriptionRepository          = (*Repository)(nil)
	_ service.UserSubscriptionCustomRepository    = (*Repository)(nil)
	_ service.UserSubscriptionBulkResetRepository = (*Repository)(nil)
)

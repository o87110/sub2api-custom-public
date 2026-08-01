package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type batchImageGroupRoutingRepo struct {
	service.BatchImageRepository

	mu               sync.Mutex
	jobs             map[string]*service.BatchImageJob
	createdGroupIDs  []int64
	submittedGroupID int64
}

func newBatchImageGroupRoutingRepo() *batchImageGroupRoutingRepo {
	return &batchImageGroupRoutingRepo{jobs: make(map[string]*service.BatchImageJob)}
}

func (r *batchImageGroupRoutingRepo) CreateBatchImageJob(
	_ context.Context,
	params service.CreateBatchImageJobParams,
) (*service.BatchImageJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	job := &service.BatchImageJob{
		BatchID:                 params.BatchID,
		UserID:                  params.UserID,
		APIKeyID:                params.APIKeyID,
		GroupID:                 params.GroupID,
		AccountID:               params.AccountID,
		Provider:                params.Provider,
		Model:                   params.Model,
		TaskName:                params.TaskName,
		Status:                  params.Status,
		ItemCount:               params.ItemCount,
		EstimatedCost:           params.EstimatedCost,
		HoldAmount:              params.HoldAmount,
		BaseUnitPrice:           params.BaseUnitPrice,
		GroupRateMultiplier:     params.GroupRateMultiplier,
		AccountRateMultiplier:   params.AccountRateMultiplier,
		BatchDiscountMultiplier: params.BatchDiscountMultiplier,
		HoldMultiplier:          params.HoldMultiplier,
		BillableUnitPrice:       params.BillableUnitPrice,
		HoldUnitPrice:           params.HoldUnitPrice,
		PricingSnapshotVersion:  params.PricingSnapshotVersion,
		Currency:                params.Currency,
		HoldID:                  params.HoldID,
		RequestHash:             params.RequestHash,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	r.jobs[job.BatchID] = job
	if job.GroupID != nil {
		r.createdGroupIDs = append(r.createdGroupIDs, *job.GroupID)
	}
	cloned := *job
	return &cloned, nil
}

func (r *batchImageGroupRoutingRepo) GetBatchImageJobByBatchID(
	_ context.Context,
	batchID string,
) (*service.BatchImageJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[batchID]
	if !ok {
		return nil, service.ErrBatchImageJobNotFound
	}
	cloned := *job
	return &cloned, nil
}

func (r *batchImageGroupRoutingRepo) BulkCreateBatchImageItems(
	context.Context,
	[]service.CreateBatchImageItemParams,
) error {
	return nil
}

func (r *batchImageGroupRoutingRepo) TransitionBatchImageJobStatus(
	_ context.Context,
	batchID, toStatus string,
	_ service.BatchImageTransitionOptions,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[batchID]
	if !ok {
		return service.ErrBatchImageJobNotFound
	}
	job.Status = toStatus
	job.UpdatedAt = time.Now()
	return nil
}

func (r *batchImageGroupRoutingRepo) TouchBatchImageJobSubmitting(context.Context, string) error {
	return nil
}

func (r *batchImageGroupRoutingRepo) UpdateBatchImageJobProviderSubmit(
	_ context.Context,
	params service.UpdateBatchImageJobProviderSubmitParams,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[params.BatchID]
	if !ok {
		return service.ErrBatchImageJobNotFound
	}
	job.Status = service.BatchImageJobStatusSubmitted
	job.ProviderJobName = &params.ProviderJobName
	job.UpdatedAt = time.Now()
	if job.GroupID != nil {
		r.submittedGroupID = *job.GroupID
	}
	return nil
}

func (r *batchImageGroupRoutingRepo) RecordBatchImageJobSubmitFailure(
	_ context.Context,
	batchID, code, message string,
	markFailed bool,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[batchID]
	if !ok {
		return service.ErrBatchImageJobNotFound
	}
	job.LastErrorCode = &code
	job.LastErrorMessage = &message
	if markFailed {
		job.Status = service.BatchImageJobStatusFailed
	}
	return nil
}

func (r *batchImageGroupRoutingRepo) MarkBatchImageJobUserDeleted(
	_ context.Context,
	_, _ int64,
	batchID string,
	deletedAt time.Time,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[batchID]
	if !ok {
		return service.ErrBatchImageJobNotFound
	}
	job.UserDeletedAt = &deletedAt
	return nil
}

type batchImageGroupRoutingAccountRepo struct {
	accountsByGroup map[int64][]service.Account
}

func (r *batchImageGroupRoutingAccountRepo) GetByID(
	_ context.Context,
	id int64,
) (*service.Account, error) {
	for _, accounts := range r.accountsByGroup {
		for i := range accounts {
			if accounts[i].ID == id {
				account := accounts[i]
				return &account, nil
			}
		}
	}
	return nil, service.ErrAccountNotFound
}

func (r *batchImageGroupRoutingAccountRepo) ListSchedulableByPlatform(
	_ context.Context,
	platform string,
) ([]service.Account, error) {
	var result []service.Account
	for _, accounts := range r.accountsByGroup {
		for _, account := range accounts {
			if account.Platform == platform {
				result = append(result, account)
			}
		}
	}
	return result, nil
}

func (r *batchImageGroupRoutingAccountRepo) ListSchedulableByGroupIDAndPlatform(
	_ context.Context,
	groupID int64,
	platform string,
) ([]service.Account, error) {
	accounts := r.accountsByGroup[groupID]
	result := make([]service.Account, 0, len(accounts))
	for _, account := range accounts {
		if account.Platform == platform {
			result = append(result, account)
		}
	}
	return result, nil
}

type batchImageGroupRoutingGroupRepo struct {
	groups map[int64]*service.Group
}

func (r *batchImageGroupRoutingGroupRepo) GetByIDLite(
	_ context.Context,
	id int64,
) (*service.Group, error) {
	group, ok := r.groups[id]
	if !ok {
		return nil, service.ErrGroupNotFound
	}
	cloned := *group
	return &cloned, nil
}

type batchImageGroupRoutingProvider struct {
	mu             sync.Mutex
	failGroupID    int64
	submitGroupIDs []int64
}

func (p *batchImageGroupRoutingProvider) Name() string {
	return service.BatchImageProviderGeminiAPI
}

func (p *batchImageGroupRoutingProvider) SupportsAccount(*service.Account) bool {
	return true
}

func (p *batchImageGroupRoutingProvider) Submit(
	_ context.Context,
	job *service.BatchImageJob,
	_ *service.Account,
	_ service.BatchImageInput,
) (*service.BatchProviderJob, error) {
	groupID := int64(0)
	if job != nil && job.GroupID != nil {
		groupID = *job.GroupID
	}
	p.mu.Lock()
	p.submitGroupIDs = append(p.submitGroupIDs, groupID)
	p.mu.Unlock()
	if groupID == p.failGroupID {
		return nil, service.ErrBatchImageProviderMissingAPIKey
	}
	return &service.BatchProviderJob{ProviderJobName: "providers/test/jobs/ok"}, nil
}

func (p *batchImageGroupRoutingProvider) Get(
	context.Context,
	*service.BatchImageJob,
	*service.Account,
) (*service.BatchProviderStatus, error) {
	return &service.BatchProviderStatus{InternalState: service.BatchProviderStateQueued}, nil
}

func (p *batchImageGroupRoutingProvider) Cancel(
	context.Context,
	*service.BatchImageJob,
	*service.Account,
) error {
	return nil
}

func (p *batchImageGroupRoutingProvider) OpenResult(
	context.Context,
	*service.BatchImageJob,
	*service.Account,
) (io.ReadCloser, string, error) {
	return io.NopCloser(strings.NewReader("")), "application/jsonl", nil
}

func (p *batchImageGroupRoutingProvider) Cleanup(
	context.Context,
	*service.BatchImageJob,
	*service.Account,
	service.CleanupTarget,
) error {
	return nil
}

type batchImageGroupRoutingPricing struct{}

func (batchImageGroupRoutingPricing) BatchImageUnitPrice(
	context.Context,
	*service.BatchImageJob,
) (float64, error) {
	return 0.25, nil
}

type batchImageGroupRoutingBilling struct {
	service.UsageBillingRepository

	mu       sync.Mutex
	reserved int
	released int
}

func (b *batchImageGroupRoutingBilling) ReserveBatchImageBalance(
	context.Context,
	*service.BatchImageBalanceHoldCommand,
) (*service.BatchImageBalanceHoldResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reserved++
	return &service.BatchImageBalanceHoldResult{Applied: true}, nil
}

func (b *batchImageGroupRoutingBilling) ReleaseBatchImageBalance(
	context.Context,
	*service.BatchImageBalanceHoldCommand,
) (*service.BatchImageBalanceHoldResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.released++
	return &service.BatchImageBalanceHoldResult{Applied: true}, nil
}

func TestBatchImageSubmitFallsBackAndPersistsActualGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		primaryGroupID   int64 = 501
		secondaryGroupID int64 = 502
		model                  = "gemini-2.5-flash-image"
	)
	primary := service.Group{
		ID:                           primaryGroupID,
		Name:                         "primary",
		Platform:                     service.PlatformGemini,
		Status:                       service.StatusActive,
		RateMultiplier:               1,
		AllowBatchImageGeneration:    true,
		BatchImageDiscountMultiplier: 0.5,
		BatchImageHoldMultiplier:     0.6,
	}
	secondary := primary
	secondary.ID = secondaryGroupID
	secondary.Name = "secondary"
	user := &service.User{ID: 71, Status: service.StatusActive, Balance: 100}
	apiKey := &service.APIKey{
		ID:       81,
		UserID:   user.ID,
		GroupID:  batchImageRoutingInt64Ptr(primaryGroupID),
		Group:    &primary,
		GroupIDs: []int64{primaryGroupID, secondaryGroupID},
		Groups:   []service.Group{primary, secondary},
		User:     user,
	}
	account := service.Account{
		ID:          91,
		Platform:    service.PlatformGemini,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":       "test-only",
			"model_mapping": map[string]any{model: model},
		},
	}
	repo := newBatchImageGroupRoutingRepo()
	provider := &batchImageGroupRoutingProvider{failGroupID: primaryGroupID}
	billingRepo := &batchImageGroupRoutingBilling{}
	cfg := &config.Config{
		RunMode: config.RunModeSimple,
		BatchImage: config.BatchImageConfig{
			Enabled: true,
		},
	}
	publicService := &service.BatchImagePublicService{
		Repo: repo,
		AccountRepo: &batchImageGroupRoutingAccountRepo{accountsByGroup: map[int64][]service.Account{
			primaryGroupID:   {account},
			secondaryGroupID: {account},
		}},
		GroupRepo: &batchImageGroupRoutingGroupRepo{groups: map[int64]*service.Group{
			primaryGroupID:   &primary,
			secondaryGroupID: &secondary,
		}},
		ProviderRegistry: service.NewBatchImageProviderRegistry(provider),
		Pricing:          batchImageGroupRoutingPricing{},
		BillingRepo:      billingRepo,
		Config:           cfg,
	}
	openAI := &OpenAIGatewayHandler{
		billingCacheService: service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil),
	}
	handler := &BatchImageHandler{service: publicService, openAI: openAI}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: user.ID, Concurrency: 1})
		c.Next()
	})
	router.POST("/v1/images/batches", handler.Submit)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/images/batches",
		strings.NewReader(`{"model":"`+model+`","provider":"gemini_api","items":[{"custom_id":"one","prompt":"draw"}]}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	provider.mu.Lock()
	require.Equal(t, []int64{primaryGroupID, secondaryGroupID}, provider.submitGroupIDs)
	provider.mu.Unlock()
	repo.mu.Lock()
	require.Equal(t, []int64{primaryGroupID, secondaryGroupID}, repo.createdGroupIDs)
	require.Equal(t, secondaryGroupID, repo.submittedGroupID)
	repo.mu.Unlock()
	billingRepo.mu.Lock()
	require.Equal(t, 2, billingRepo.reserved, "每个实际尝试的分组各冻结一次")
	require.Equal(t, 1, billingRepo.released, "失败分组的冻结必须在跨组前释放")
	billingRepo.mu.Unlock()
}

func batchImageRoutingInt64Ptr(value int64) *int64 {
	return &value
}

var _ service.BatchImageRepository = (*batchImageGroupRoutingRepo)(nil)
var _ service.BatchImageAccountSelectionRepository = (*batchImageGroupRoutingAccountRepo)(nil)
var _ service.BatchImageGroupPricingRepository = (*batchImageGroupRoutingGroupRepo)(nil)
var _ service.BatchImageProvider = (*batchImageGroupRoutingProvider)(nil)
var _ service.BatchImagePricingResolver = batchImageGroupRoutingPricing{}
var _ service.UsageBillingRepository = (*batchImageGroupRoutingBilling)(nil)

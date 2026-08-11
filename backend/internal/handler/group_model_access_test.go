package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/custom/groupmodelaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBindGroupModelAccessChannelMappingRejectsBlockedTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := groupmodelaccess.WithPolicy(context.Background(), groupmodelaccess.NewPolicy([]string{"gpt-5.4-mini"}))
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)

	ok := bindGroupModelAccessChannelMapping(c, service.ChannelMappingResult{
		Mapped: true, MappedModel: "gpt-5.4-mini",
	})

	require.False(t, ok)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Equal(t, "model_not_found", gjson.Get(recorder.Body.String(), "error.code").String())
}

func TestWithGroupModelAccessChannelMappingReplacesPriorTurnModel(t *testing.T) {
	ctx := groupmodelaccess.WithRequestModel(context.Background(), "first-turn-model")

	ctx = withGroupModelAccessChannelMapping(ctx, service.ChannelMappingResult{
		Mapped: false, MappedModel: "second-turn-model",
	})

	require.Equal(t, "second-turn-model", groupmodelaccess.RequestModel(ctx, "fallback-model"))
}

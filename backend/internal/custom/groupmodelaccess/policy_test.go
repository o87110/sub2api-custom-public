package groupmodelaccess

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeAndExactMatching(t *testing.T) {
	normalized := Normalize([]string{" gpt-5.6-luna ", "", "gpt-5.4-mini", "gpt-5.6-luna"})
	require.Equal(t, []string{"gpt-5.6-luna", "gpt-5.4-mini"}, normalized)

	policy := NewPolicy(normalized)
	require.True(t, policy.Blocks("gpt-5.6-luna"))
	require.True(t, policy.Blocks(" gpt-5.4-mini "))
	require.False(t, policy.Blocks("gpt-5.6-sol"))
	require.False(t, policy.Blocks("gpt-5.4-mini-high"))
}

func TestPolicyContext(t *testing.T) {
	ctx := WithPolicy(context.Background(), NewPolicy([]string{"gpt-5.4-mini"}))
	require.True(t, BlocksContext(ctx, "gpt-5.4-mini"))
	require.False(t, BlocksContext(context.Background(), "gpt-5.4-mini"))
}

func TestBindAndInspectRequestRestoresJSONBody(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "top level model", body: " {\n  \"model\": \"gpt-5.4-mini\", \"input\": []\n}"},
		{name: "live session model", body: `{"session":{"model":"gpt-5.4-mini"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")

			model, blocked, err := BindAndInspectRequest(req, []string{"gpt-5.4-mini"})

			require.NoError(t, err)
			require.Equal(t, "gpt-5.4-mini", model)
			require.True(t, blocked)
			restored, readErr := io.ReadAll(req.Body)
			require.NoError(t, readErr)
			require.Equal(t, []byte(tt.body), restored)
			require.Equal(t, int64(len(tt.body)), req.ContentLength)
			require.True(t, BlocksContext(req.Context(), "gpt-5.4-mini"))
		})
	}
}

func TestBindAndInspectRequestRestoresMultipartBody(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("prompt", "draw a moon"))
	require.NoError(t, writer.WriteField("model", "gpt-5.6-luna"))
	require.NoError(t, writer.Close())
	original := append([]byte(nil), body.Bytes()...)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(original))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	model, blocked, err := BindAndInspectRequest(req, []string{"gpt-5.6-luna"})

	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-luna", model)
	require.True(t, blocked)
	restored, readErr := io.ReadAll(req.Body)
	require.NoError(t, readErr)
	require.Equal(t, original, restored)
}

func TestBindAndInspectRequestReadsPathAndQueryModels(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "gemini path", url: "/v1beta/models/gpt-5.4-mini:streamGenerateContent?alt=sse"},
		{name: "query", url: "/v1/realtime?model=gpt-5.4-mini"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.url, nil)
			model, blocked, err := BindAndInspectRequest(req, []string{"gpt-5.4-mini"})
			require.NoError(t, err)
			require.Equal(t, "gpt-5.4-mini", model)
			require.True(t, blocked)
		})
	}
}

func TestWriteBlockedResponseUsesProtocolShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		path       string
		assertBody func(*testing.T, string)
	}{
		{
			name: "openai", path: "/v1/responses",
			assertBody: func(t *testing.T, body string) {
				require.Equal(t, "model_not_found", gjson.Get(body, "error.code").String())
				require.Equal(t, "model", gjson.Get(body, "error.param").String())
			},
		},
		{
			name: "anthropic", path: "/v1/messages",
			assertBody: func(t *testing.T, body string) {
				require.Equal(t, "error", gjson.Get(body, "type").String())
				require.Equal(t, "not_found_error", gjson.Get(body, "error.type").String())
			},
		},
		{
			name: "google", path: "/v1beta/models/gpt-5.4-mini:generateContent",
			assertBody: func(t *testing.T, body string) {
				require.Equal(t, int64(http.StatusNotFound), gjson.Get(body, "error.code").Int())
				require.Equal(t, "NOT_FOUND", gjson.Get(body, "error.status").String())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil)

			WriteBlockedResponse(c, "gpt-5.4-mini")

			require.Equal(t, http.StatusNotFound, recorder.Code)
			tt.assertBody(t, recorder.Body.String())
		})
	}
}

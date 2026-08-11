package groupmodelaccess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type policyContextKey struct{}
type requestModelContextKey struct{}
type fallbackModelContextKey struct{}

var ErrModelBlocked = errors.New("model blocked by group policy")

type BlockedModelError struct {
	Model string
}

func (e *BlockedModelError) Error() string {
	return fmt.Sprintf("model %q is blocked by group policy", e.Model)
}

func (e *BlockedModelError) Unwrap() error {
	return ErrModelBlocked
}

// Policy is an immutable exact-match model blocklist bound to one request.
type Policy struct {
	models map[string]struct{}
}

// Normalize trims model IDs, removes empty values and preserves first-seen order.
func Normalize(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, raw := range models {
		model := strings.TrimSpace(raw)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func NewPolicy(models []string) Policy {
	normalized := Normalize(models)
	if len(normalized) == 0 {
		return Policy{}
	}
	set := make(map[string]struct{}, len(normalized))
	for _, model := range normalized {
		set[model] = struct{}{}
	}
	return Policy{models: set}
}

func (p Policy) Blocks(model string) bool {
	if len(p.models) == 0 {
		return false
	}
	_, ok := p.models[strings.TrimSpace(model)]
	return ok
}

func (p Policy) Empty() bool {
	return len(p.models) == 0
}

// Union returns a policy containing the exact-match entries from both inputs.
func (p Policy) Union(other Policy) Policy {
	if p.Empty() {
		return other.clone()
	}
	if other.Empty() {
		return p.clone()
	}
	models := make(map[string]struct{}, len(p.models)+len(other.models))
	for model := range p.models {
		models[model] = struct{}{}
	}
	for model := range other.models {
		models[model] = struct{}{}
	}
	return Policy{models: models}
}

func (p Policy) clone() Policy {
	if p.Empty() {
		return Policy{}
	}
	models := make(map[string]struct{}, len(p.models))
	for model := range p.models {
		models[model] = struct{}{}
	}
	return Policy{models: models}
}

func Blocks(models []string, model string) bool {
	return NewPolicy(models).Blocks(model)
}

func WithPolicy(ctx context.Context, policy Policy) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, policyContextKey{}, policy)
}

// WithAdditionalModels adds another group's blocklist without discarding the
// authenticated group's policy. This is used by fallback routing.
func WithAdditionalModels(ctx context.Context, models []string) context.Context {
	return WithPolicy(ctx, FromContext(ctx).Union(NewPolicy(models)))
}

func FromContext(ctx context.Context) Policy {
	if ctx == nil {
		return Policy{}
	}
	policy, _ := ctx.Value(policyContextKey{}).(Policy)
	return policy
}

func BlocksContext(ctx context.Context, model string) bool {
	return FromContext(ctx).Blocks(model)
}

func Check(ctx context.Context, model string) error {
	model = strings.TrimSpace(model)
	if model == "" || !BlocksContext(ctx, model) {
		return nil
	}
	return &BlockedModelError{Model: model}
}

// WithRequestModel stores a deterministic group/channel/composite rewrite.
// Account-level mapping must be applied after this model.
func WithRequestModel(ctx context.Context, model string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestModelContextKey{}, strings.TrimSpace(model))
}

func RequestModel(ctx context.Context, fallback string) string {
	if ctx != nil {
		if model, _ := ctx.Value(requestModelContextKey{}).(string); strings.TrimSpace(model) != "" {
			return strings.TrimSpace(model)
		}
	}
	return strings.TrimSpace(fallback)
}

// WithFallbackModel stores a mapping used only when account-level mapping did
// not match, as required by the Messages dispatch pipeline.
func WithFallbackModel(ctx context.Context, model string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, fallbackModelContextKey{}, strings.TrimSpace(model))
}

func FallbackModel(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	model, _ := ctx.Value(fallbackModelContextKey{}).(string)
	return strings.TrimSpace(model)
}

// BindAndInspectRequest binds the group policy and returns the client model
// before any internal rewrite. Request bodies are restored byte-for-byte.
func BindAndInspectRequest(req *http.Request, blockedModels []string) (string, bool, error) {
	if req == nil {
		return "", false, nil
	}
	policy := NewPolicy(blockedModels)
	reqWithPolicy := req.WithContext(WithPolicy(req.Context(), policy))
	*req = *reqWithPolicy
	if policy.Empty() {
		return "", false, nil
	}

	if model := modelFromURL(req); model != "" {
		return model, policy.Blocks(model), nil
	}
	if req.Body == nil || req.Method == http.MethodGet || req.Method == http.MethodHead {
		return "", false, nil
	}

	body, err := io.ReadAll(req.Body)
	if closeErr := req.Body.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	restoreRequestBody(req, body)
	if err != nil {
		return "", false, err
	}

	model := modelFromBody(req.Header.Get("Content-Type"), body)
	return model, policy.Blocks(model), nil
}

func modelFromURL(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	if model := strings.TrimSpace(req.URL.Query().Get("model")); model != "" {
		return model
	}
	path := req.URL.Path
	marker := "/models/"
	idx := strings.Index(path, marker)
	if idx < 0 {
		return ""
	}
	modelAction := strings.TrimPrefix(path[idx+len(marker):], "/")
	if modelAction == "" {
		return ""
	}
	if slash := strings.IndexByte(modelAction, '/'); slash >= 0 {
		modelAction = modelAction[:slash]
	}
	if colon := strings.LastIndex(modelAction, ":"); colon >= 0 {
		modelAction = modelAction[:colon]
	}
	return strings.TrimSpace(modelAction)
}

func modelFromBody(contentType string, body []byte) string {
	mediaType, params, _ := mime.ParseMediaType(strings.TrimSpace(contentType))
	if strings.EqualFold(mediaType, "multipart/form-data") {
		return multipartModel(body, strings.TrimSpace(params["boundary"]))
	}
	if model := strings.TrimSpace(gjson.GetBytes(body, "model").String()); model != "" {
		return model
	}
	return strings.TrimSpace(gjson.GetBytes(body, "session.model").String())
}

func multipartModel(body []byte, boundary string) string {
	if boundary == "" {
		return ""
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) || err != nil {
			return ""
		}
		if part.FormName() != "model" || part.FileName() != "" {
			continue
		}
		value, err := io.ReadAll(part)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(value))
	}
}

func restoreRequestBody(req *http.Request, body []byte) {
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

func WriteBlockedResponse(c *gin.Context, model string) {
	model = strings.TrimSpace(model)
	message := fmt.Sprintf("The model %q does not exist or is not available for this group", model)
	path := ""
	if c != nil && c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}

	switch {
	case strings.Contains(path, "/v1beta/"):
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{
			"code": http.StatusNotFound, "message": message, "status": "NOT_FOUND",
		}})
	case strings.Contains(path, "/messages"):
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
			"type":  "error",
			"error": gin.H{"type": "not_found_error", "message": message},
		})
	default:
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{
			"message": message, "type": "invalid_request_error", "param": "model", "code": "model_not_found",
		}})
	}
}

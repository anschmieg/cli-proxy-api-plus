package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	copilotauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/copilot"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	githubCopilotBaseURL          = "https://api.githubcopilot.com"
	githubModelsBaseURL           = "https://models.github.ai"
	githubCopilotChatPath         = "/chat/completions"
	githubCopilotResponsesPath    = "/responses"
	githubModelsEmbeddingsPath    = "/inference/embeddings"
	githubCopilotAuthType         = "github-copilot"
	githubCopilotTokenCacheTTL = 25 * time.Minute
	// tokenExpiryBuffer is the time before expiry when we should refresh the token.
	tokenExpiryBuffer = 5 * time.Minute
	// maxScannerBufferSize is the maximum buffer size for SSE scanning (20MB).
	maxScannerBufferSize = 20_971_520

	// Copilot API header values.
	copilotUserAgent     = "GithubCopilot/1.388.0"
	copilotEditorVersion = "vscode/1.108.0"
	copilotPluginVersion = "copilot-chat/0.36.2"
	copilotIntegrationID = "vscode-chat"
	copilotOpenAIIntent  = "conversation-panel"
)

// GitHubCopilotExecutor handles requests to the GitHub Copilot API.
type GitHubCopilotExecutor struct {
	cfg   *config.Config
	mu    sync.RWMutex
	cache map[string]*cachedAPIToken
}

// cachedAPIToken stores a cached Copilot API token with its expiry.
type cachedAPIToken struct {
	token     string
	expiresAt time.Time
}

// NewGitHubCopilotExecutor constructs a new executor instance.
func NewGitHubCopilotExecutor(cfg *config.Config) *GitHubCopilotExecutor {
	return &GitHubCopilotExecutor{
		cfg:   cfg,
		cache: make(map[string]*cachedAPIToken),
	}
}

// Identifier implements ProviderExecutor.
func (e *GitHubCopilotExecutor) Identifier() string { return githubCopilotAuthType }

// PrepareRequest implements ProviderExecutor.
func (e *GitHubCopilotExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	apiToken, errToken := e.ensureAPIToken(ctx, auth)
	if errToken != nil {
		return errToken
	}
	e.applyHeaders(req, apiToken)
	return nil
}

// HttpRequest injects GitHub Copilot credentials into the request and executes it.
func (e *GitHubCopilotExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("github-copilot executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if errPrepare := e.PrepareRequest(httpReq, auth); errPrepare != nil {
		return nil, errPrepare
	}
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// Execute handles non-streaming requests to GitHub Copilot.
func (e *GitHubCopilotExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	// Determine endpoint kind first to choose correct token
	endpointKind := githubCopilotEndpointForModel(req.Model)

	// For embeddings (GitHub Models), use the GitHub OAuth access token directly
	// For chat/completions (GitHub Copilot), use the Copilot API token
	var apiToken string
	if endpointKind == githubEndpointEmbeddings {
		// GitHub Models requires the original GitHub OAuth token
		if auth == nil {
			return resp, statusErr{code: http.StatusUnauthorized, msg: "missing auth"}
		}
		apiToken = metaStringValue(auth.Metadata, "access_token")
		if apiToken == "" {
			return resp, statusErr{code: http.StatusUnauthorized, msg: "missing github access token for embeddings"}
		}
	} else {
		var errToken error
		apiToken, errToken = e.ensureAPIToken(ctx, auth)
		if errToken != nil {
			return resp, errToken
		}
	}

	reporter := newUsageReporter(ctx, e.Identifier(), req.Model, auth)
	defer reporter.trackFailure(ctx, &err)

	from := opts.SourceFormat
	useResponses := useGitHubCopilotResponsesEndpoint(from)
	to := sdktranslator.FromString("openai")
	if useResponses {
		to = sdktranslator.FromString("openai-response")
	}
	originalPayload := bytes.Clone(req.Payload)
	if len(opts.OriginalRequest) > 0 {
		originalPayload = bytes.Clone(opts.OriginalRequest)
	}
	originalTranslated := sdktranslator.TranslateRequest(from, to, req.Model, originalPayload, false)
	body := sdktranslator.TranslateRequest(from, to, req.Model, bytes.Clone(req.Payload), false)
	body = e.normalizeModel(req.Model, body)
	requestedModel := payloadRequestedModel(opts, req.Model)
	body = applyPayloadConfigWithRoot(e.cfg, req.Model, to.String(), "", body, originalTranslated, requestedModel)
	body, _ = sjson.SetBytes(body, "stream", false)

	path := githubCopilotChatPath
	baseURL := githubCopilotBaseURL
	if endpointKind == githubEndpointEmbeddings {
		path = githubModelsEmbeddingsPath
		baseURL = githubModelsBaseURL
	} else if useResponses {
		path = githubCopilotResponsesPath
	}
	url := baseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return resp, err
	}
	e.applyHeaders(httpReq, apiToken)
	if endpointKind == githubEndpointEmbeddings {
		// GitHub Models requires different headers
		httpReq.Header.Set("Accept", "application/vnd.github+json")
		httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("github-copilot executor: close response body error: %v", errClose)
		}
	}()

	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	if !isHTTPSuccess(httpResp.StatusCode) {
		data, _ := io.ReadAll(httpResp.Body)
		appendAPIResponseChunk(ctx, e.cfg, data)
		log.Debugf("github-copilot executor: upstream error status: %d, body: %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), data))
		err = statusErr{code: httpResp.StatusCode, msg: string(data)}
		return resp, err
	}

	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	appendAPIResponseChunk(ctx, e.cfg, data)

	detail := parseOpenAIUsage(data)
	if useResponses && detail.TotalTokens == 0 {
		detail = parseOpenAIResponsesUsage(data)
	}
	if detail.TotalTokens > 0 {
		reporter.publish(ctx, detail)
	}

	var param any
	converted := sdktranslator.TranslateNonStream(ctx, to, from, req.Model, bytes.Clone(opts.OriginalRequest), body, data, &param)
	resp = cliproxyexecutor.Response{Payload: []byte(converted)}
	reporter.ensurePublished(ctx)
	return resp, nil
}

// ExecuteStream handles streaming requests to GitHub Copilot.
func (e *GitHubCopilotExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (stream <-chan cliproxyexecutor.StreamChunk, err error) {
	// Determine endpoint kind first to choose correct token
	endpointKind := githubCopilotEndpointForModel(req.Model)

	// Embeddings doesn't support streaming
	if endpointKind == githubEndpointEmbeddings {
		return nil, statusErr{code: http.StatusBadRequest, msg: "embeddings endpoint does not support streaming"}
	}

	apiToken, errToken := e.ensureAPIToken(ctx, auth)
	if errToken != nil {
		return nil, errToken
	}

	reporter := newUsageReporter(ctx, e.Identifier(), req.Model, auth)
	defer reporter.trackFailure(ctx, &err)

	from := opts.SourceFormat
	useResponses := useGitHubCopilotResponsesEndpoint(from)
	to := sdktranslator.FromString("openai")
	if useResponses {
		to = sdktranslator.FromString("openai-response")
	}
	originalPayload := bytes.Clone(req.Payload)
	if len(opts.OriginalRequest) > 0 {
		originalPayload = bytes.Clone(opts.OriginalRequest)
	}
	originalTranslated := sdktranslator.TranslateRequest(from, to, req.Model, originalPayload, false)
	body := sdktranslator.TranslateRequest(from, to, req.Model, bytes.Clone(req.Payload), true)
	body = e.normalizeModel(req.Model, body)
	requestedModel := payloadRequestedModel(opts, req.Model)
	body = applyPayloadConfigWithRoot(e.cfg, req.Model, to.String(), "", body, originalTranslated, requestedModel)
	body, _ = sjson.SetBytes(body, "stream", true)
	// Enable stream options for usage stats in stream
	if !useResponses {
		body, _ = sjson.SetBytes(body, "stream_options.include_usage", true)
	}

	// Embeddings don't support streaming
	if githubCopilotEndpointForModel(req.Model) == githubEndpointEmbeddings {
		return nil, statusErr{code: http.StatusBadRequest, msg: "embeddings endpoint does not support streaming"}
	}

	path := githubCopilotChatPath
	if useResponses {
		path = githubCopilotResponsesPath
	}
	url := githubCopilotBaseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	e.applyHeaders(httpReq, apiToken)

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return nil, err
	}

	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	if !isHTTPSuccess(httpResp.StatusCode) {
		data, readErr := io.ReadAll(httpResp.Body)
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("github-copilot executor: close response body error: %v", errClose)
		}
		if readErr != nil {
			recordAPIResponseError(ctx, e.cfg, readErr)
			return nil, readErr
		}
		appendAPIResponseChunk(ctx, e.cfg, data)
		log.Debugf("github-copilot executor: upstream error status: %d, body: %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), data))
		err = statusErr{code: httpResp.StatusCode, msg: string(data)}
		return nil, err
	}

	out := make(chan cliproxyexecutor.StreamChunk)
	stream = out

	go func() {
		defer close(out)
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("github-copilot executor: close response body error: %v", errClose)
			}
		}()

		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, maxScannerBufferSize)
		var param any

		for scanner.Scan() {
			line := scanner.Bytes()
			appendAPIResponseChunk(ctx, e.cfg, line)

			// Parse SSE data
			if bytes.HasPrefix(line, dataTag) {
				data := bytes.TrimSpace(line[5:])
				if bytes.Equal(data, []byte("[DONE]")) {
					continue
				}
				if detail, ok := parseOpenAIStreamUsage(line); ok {
					reporter.publish(ctx, detail)
				} else if useResponses {
					if detail, ok := parseOpenAIResponsesStreamUsage(line); ok {
						reporter.publish(ctx, detail)
					}
				}
			}

			chunks := sdktranslator.TranslateStream(ctx, to, from, req.Model, bytes.Clone(opts.OriginalRequest), body, bytes.Clone(line), &param)
			for i := range chunks {
				out <- cliproxyexecutor.StreamChunk{Payload: []byte(chunks[i])}
			}
		}

		if errScan := scanner.Err(); errScan != nil {
			recordAPIResponseError(ctx, e.cfg, errScan)
			reporter.publishFailure(ctx)
			out <- cliproxyexecutor.StreamChunk{Err: errScan}
		} else {
			reporter.ensurePublished(ctx)
		}
	}()

	return stream, nil
}

// CountTokens approximates prompt tokens for GitHub Copilot requests.
func (e *GitHubCopilotExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_, errToken := e.ensureAPIToken(ctx, auth)
	if errToken != nil {
		return cliproxyexecutor.Response{}, errToken
	}

	from := opts.SourceFormat
	useResponses := useGitHubCopilotResponsesEndpoint(from)
	to := sdktranslator.FromString("openai") // Assume OpenAI token counting logic for Copilot
	if useResponses {
		to = sdktranslator.FromString("openai-response")
	}

	body := sdktranslator.TranslateRequest(from, to, req.Model, bytes.Clone(req.Payload), false)
	body = e.normalizeModel(req.Model, body)

	// Use OpenAI chat token counting, as Copilot mostly aligns with it
	enc, err := GetTokenizer(req.Model)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("github-copilot executor: tokenizer init failed: %w", err)
	}

	count, err := countOpenAIChatTokens(enc, body)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("github-copilot executor: token counting failed: %w", err)
	}

	usageJSON := fmt.Sprintf(`{"usage":{"prompt_tokens":%d,"completion_tokens":0,"total_tokens":%d}}`, count, count)
	translated := sdktranslator.TranslateTokenCount(ctx, to, from, count, []byte(usageJSON))
	return cliproxyexecutor.Response{Payload: []byte(translated)}, nil
}

// Refresh validates the GitHub token is still working.
// GitHub OAuth tokens don't expire traditionally, so we just validate.
func (e *GitHubCopilotExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil {
		return nil, statusErr{code: http.StatusUnauthorized, msg: "missing auth"}
	}

	// Get the GitHub access token
	accessToken := metaStringValue(auth.Metadata, "access_token")
	if accessToken == "" {
		return auth, nil
	}

	// Validate the token can still get a Copilot API token
	copilotAuth := copilotauth.NewCopilotAuth(e.cfg)
	_, err := copilotAuth.GetCopilotAPIToken(ctx, accessToken)
	if err != nil {
		return nil, statusErr{code: http.StatusUnauthorized, msg: fmt.Sprintf("github-copilot token validation failed: %v", err)}
	}

	return auth, nil
}

// ensureAPIToken gets or refreshes the Copilot API token.
func (e *GitHubCopilotExecutor) ensureAPIToken(ctx context.Context, auth *cliproxyauth.Auth) (string, error) {
	if auth == nil {
		return "", statusErr{code: http.StatusUnauthorized, msg: "missing auth"}
	}

	// Get the GitHub access token
	accessToken := metaStringValue(auth.Metadata, "access_token")
	if accessToken == "" {
		return "", statusErr{code: http.StatusUnauthorized, msg: "missing github access token"}
	}

	// Check for cached API token using thread-safe access
	e.mu.RLock()
	if cached, ok := e.cache[accessToken]; ok && cached.expiresAt.After(time.Now().Add(tokenExpiryBuffer)) {
		e.mu.RUnlock()
		return cached.token, nil
	}
	e.mu.RUnlock()

	// Get a new Copilot API token
	copilotAuth := copilotauth.NewCopilotAuth(e.cfg)
	apiToken, err := copilotAuth.GetCopilotAPIToken(ctx, accessToken)
	if err != nil {
		return "", statusErr{code: http.StatusUnauthorized, msg: fmt.Sprintf("failed to get copilot api token: %v", err)}
	}

	// Cache the token with thread-safe access
	expiresAt := time.Now().Add(githubCopilotTokenCacheTTL)
	if apiToken.ExpiresAt > 0 {
		expiresAt = time.Unix(apiToken.ExpiresAt, 0)
	}
	e.mu.Lock()
	e.cache[accessToken] = &cachedAPIToken{
		token:     apiToken.Token,
		expiresAt: expiresAt,
	}
	e.mu.Unlock()

	return apiToken.Token, nil
}

// applyHeaders sets the required headers for GitHub Copilot API requests.
func (e *GitHubCopilotExecutor) applyHeaders(r *http.Request, apiToken string) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+apiToken)
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", copilotUserAgent)
	r.Header.Set("Editor-Version", copilotEditorVersion)
	r.Header.Set("Editor-Plugin-Version", copilotPluginVersion)
	r.Header.Set("Openai-Intent", copilotOpenAIIntent)
	r.Header.Set("Copilot-Integration-Id", copilotIntegrationID)
	r.Header.Set("X-Request-Id", uuid.NewString())
}

// normalizeModel rewrites embedding model aliases to the GitHub Models canonical id.
func (e *GitHubCopilotExecutor) normalizeModel(model string, body []byte) []byte {
	if githubCopilotEndpointForModel(model) != githubEndpointEmbeddings {
		return body
	}
	current := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "model").String()))
	if current == "text-embedding-3-small" {
		if updated, err := sjson.SetBytes(body, "model", "openai/text-embedding-3-small"); err == nil {
			return updated
		}
	}
	return body
}

func useGitHubCopilotResponsesEndpoint(sourceFormat sdktranslator.Format) bool {
	return sourceFormat.String() == "openai-response"
}

type githubEndpointKind string

const (
	githubEndpointChat       githubEndpointKind = "chat"
	githubEndpointEmbeddings githubEndpointKind = "embeddings"
)

func githubCopilotEndpointForModel(model string) githubEndpointKind {
	m := strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(m, "embedding") {
		return githubEndpointEmbeddings
	}
	return githubEndpointChat
}

// isHTTPSuccess checks if the status code indicates success (2xx).
func isHTTPSuccess(statusCode int) bool {
	return statusCode >= 200 && statusCode < 300
}

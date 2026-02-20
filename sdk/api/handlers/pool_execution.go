package handlers

import (
	"context"
	"fmt"
	"net/http"

	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/pool"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
)

// isPoolFailoverError returns true if the status code indicates all credentials for a provider
// are exhausted or cooling down, meaning the next pool member should be tried.
// 429 (rate limited/cooling down) and 503 (service unavailable/quota) are retriable.
func isPoolFailoverError(status int) bool {
	switch status {
	case http.StatusTooManyRequests, // 429
		http.StatusServiceUnavailable: // 503
		return true
	}
	return false
}

// resolvedModelWithSuffix re-applies a thinking suffix to a pool target's model name,
// preserving any suffix the user included in their original request.
func resolvedModelWithSuffix(targetModel string, suffix thinking.SuffixResult) string {
	if suffix.HasSuffix {
		return fmt.Sprintf("%s(%s)", targetModel, suffix.RawSuffix)
	}
	return targetModel
}

// poolOrClusterResolution checks if modelName is a pool/cluster and returns its targets.
// Returns nil + zero SuffixResult if not a pool/cluster or if the resolver is unavailable.
func (h *BaseAPIHandler) poolOrClusterResolution(modelName string) (*pool.Resolution, thinking.SuffixResult) {
	if h.PoolResolver == nil {
		return nil, thinking.SuffixResult{}
	}
	suffix := thinking.ParseSuffix(modelName)
	res := h.PoolResolver.Resolve(suffix.ModelName)
	if !res.Matched || len(res.Targets) == 0 {
		return nil, thinking.SuffixResult{}
	}
	return &res, suffix
}

// buildPoolExecutionRequest builds the coreexecutor.Request and Options for a given target model,
// preserving the original payload while rewriting the model field.
func buildPoolExecutionRequest(targetModel string, rawJSON []byte, handlerType string, stream bool, alt string) (coreexecutor.Request, coreexecutor.Options) {
	req := coreexecutor.Request{
		Model:   targetModel,
		Payload: cloneBytes(rawJSON),
	}
	opts := coreexecutor.Options{
		Stream:          stream,
		Alt:             alt,
		OriginalRequest: cloneBytes(rawJSON),
		SourceFormat:    sdktranslator.FromString(handlerType),
	}
	return req, opts
}

// resolveTargetProviders returns the providers for a pool target.
// Uses the explicitly configured providers list, falling back to registry lookup.
func resolveTargetProviders(target pool.ResolvedTarget) []string {
	if len(target.Providers) > 0 {
		return target.Providers
	}
	return util.GetProviderName(target.Model)
}

// ExecuteWithPoolFailover executes a non-streaming request with pool-aware failover.
// If modelName resolves to a pool or cluster, members are tried in priority order.
// When a member's providers return a retriable error (429/503), the next member is tried.
// Returns (response, error, handled). handled=false means the model is not a pool/cluster;
// the caller should fall back to the standard Execute path.
func (h *BaseAPIHandler) ExecuteWithPoolFailover(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, *interfaces.ErrorMessage, bool) {
	resolution, suffix := h.poolOrClusterResolution(modelName)
	if resolution == nil {
		return nil, nil, false
	}

	targets := resolution.Targets
	var lastErr *interfaces.ErrorMessage

	for i, target := range targets {
		resolvedModel := resolvedModelWithSuffix(target.Model, suffix)
		req, opts := buildPoolExecutionRequest(resolvedModel, rawJSON, handlerType, false, alt)

		reqMeta := requestExecutionMetadata(ctx)
		reqMeta[coreexecutor.RequestedModelMetadataKey] = resolvedModel
		opts.Metadata = reqMeta

		providers := resolveTargetProviders(target)
		if len(providers) == 0 {
			// Skip: no resolvable providers for this member
			continue
		}

		resp, err := h.AuthManager.Execute(ctx, providers, req, opts)
		if err != nil {
			status := statusFromError(err)
			var addon http.Header
			if he, ok := err.(interface{ Headers() http.Header }); ok && he != nil {
				if hdr := he.Headers(); hdr != nil {
					addon = hdr.Clone()
				}
			}
			lastErr = &interfaces.ErrorMessage{StatusCode: status, Error: err, Addon: addon}

			if isPoolFailoverError(status) && i < len(targets)-1 {
				// Retriable error and more members remain: try next
				continue
			}
			// Non-retriable, or last member
			return nil, lastErr, true
		}

		return cloneBytes(resp.Payload), nil, true
	}

	if lastErr != nil {
		return nil, lastErr, true
	}
	return nil, &interfaces.ErrorMessage{
		StatusCode: http.StatusServiceUnavailable,
		Error:      fmt.Errorf("all pool members exhausted for model %q", modelName),
	}, true
}

// ExecuteStreamWithPoolFailover executes a streaming request with pool-aware failover.
// When the first pool member is exhausted before any bytes are sent, it transparently
// tries the next member. Returns (dataChan, errChan, handled bool).
// handled=false means the model is not a pool/cluster; caller should use the standard path.
func (h *BaseAPIHandler) ExecuteStreamWithPoolFailover(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) (<-chan []byte, <-chan *interfaces.ErrorMessage, bool) {
	resolution, suffix := h.poolOrClusterResolution(modelName)
	if resolution == nil {
		return nil, nil, false
	}

	targets := resolution.Targets
	dataChan := make(chan []byte)
	errChan := make(chan *interfaces.ErrorMessage, 1)

	go func() {
		defer close(dataChan)
		defer close(errChan)

		for i, target := range targets {
			resolvedModel := resolvedModelWithSuffix(target.Model, suffix)
			req, opts := buildPoolExecutionRequest(resolvedModel, rawJSON, handlerType, true, alt)

			reqMeta := requestExecutionMetadata(ctx)
			reqMeta[coreexecutor.RequestedModelMetadataKey] = resolvedModel
			opts.Metadata = reqMeta

			providers := resolveTargetProviders(target)
			if len(providers) == 0 {
				continue
			}

			chunks, err := h.AuthManager.ExecuteStream(ctx, providers, req, opts)
			if err != nil {
				status := statusFromError(err)
				if isPoolFailoverError(status) && i < len(targets)-1 {
					continue
				}
				var addon http.Header
				if he, ok := err.(interface{ Headers() http.Header }); ok && he != nil {
					if hdr := he.Headers(); hdr != nil {
						addon = hdr.Clone()
					}
				}
				errChan <- &interfaces.ErrorMessage{StatusCode: status, Error: err, Addon: addon}
				return
			}

			sentPayload := false
			memberExhausted := false

		streamLoop:
			for {
				var chunk coreexecutor.StreamChunk
				var ok bool
				if ctx != nil {
					select {
					case <-ctx.Done():
						return
					case chunk, ok = <-chunks:
					}
				} else {
					chunk, ok = <-chunks
				}
				if !ok {
					// Channel closed cleanly — stream done
					return
				}
				if chunk.Err != nil {
					// If no payload sent yet and this is a retriable error with more members, fail over
					if !sentPayload {
						status := statusFromError(chunk.Err)
						if isPoolFailoverError(status) && i < len(targets)-1 {
							memberExhausted = true
							break streamLoop
						}
					}
					// Payload already sent, or non-retriable error: propagate
					streamStatus := statusFromError(chunk.Err)
					if streamStatus == 0 {
						streamStatus = http.StatusInternalServerError
					}
					errChan <- &interfaces.ErrorMessage{StatusCode: streamStatus, Error: chunk.Err}
					return
				}
				sentPayload = true
				select {
				case <-ctx.Done():
					return
				case dataChan <- cloneBytes(chunk.Payload):
				}
			}

			if !memberExhausted {
				// Clean exit
				return
			}
			// memberExhausted=true: continue loop to try next target
		}

		// All members failed before sending any data
		errChan <- &interfaces.ErrorMessage{
			StatusCode: http.StatusServiceUnavailable,
			Error:      fmt.Errorf("all pool members exhausted for model %q", modelName),
		}
	}()

	return dataChan, errChan, true
}

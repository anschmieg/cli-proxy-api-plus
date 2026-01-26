package github_copilot_access

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const (
	// AccessProviderTypeGitHubCopilot is the type identifier for the GitHub Copilot access provider.
	AccessProviderTypeGitHubCopilot = "github-copilot"
	// githubCopilotUserAgentPrefix is the expected User-Agent prefix from GitHub Copilot clients.
	githubCopilotUserAgentPrefix = "github-copilot-cli"
)

var (
	registerOnce sync.Once
	// Reference to the global coreauth.Manager. This is a bit hacky,
	// ideally access to AuthManager would be passed more cleanly.
	globalAuthManager *coreauth.Manager
)

// SetGlobalAuthManager allows setting the global coreauth.Manager for this package.
func SetGlobalAuthManager(mgr *coreauth.Manager) {
	globalAuthManager = mgr
}

// Register ensures the GitHub Copilot access provider is available to the access manager.
func Register() {
	registerOnce.Do(func() {
		sdkaccess.RegisterProvider(AccessProviderTypeGitHubCopilot, newProvider)
		log.Debugf("Registered GitHub Copilot access provider.")
	})
}

// provider implements the sdkaccess.Provider interface for GitHub Copilot.
type provider struct {
	cfg *config.Config // Reference to the main app config
}

func newProvider(cfg *sdkconfig.AccessProvider, sdkCfg *sdkconfig.SDKConfig) (sdkaccess.Provider, error) {
	// Reconstruct the full app config from sdkConfig. This assumes sdkConfig is a subset of config.Config.
	// This might need refinement depending on how full config is typically passed.
	appCfg := &config.Config{SDKConfig: *sdkCfg}

	// This provider doesn't directly use cfg.Config, but its Authenticate method will.
	return &provider{cfg: appCfg}, nil
}

func (p *provider) Identifier() string {
	return AccessProviderTypeGitHubCopilot
}

func (p *provider) Authenticate(ctx context.Context, r *http.Request) (*sdkaccess.Result, error) {
	if p == nil {
		return nil, sdkaccess.ErrNotHandled
	}

	userAgent := r.Header.Get("User-Agent")
	if !strings.HasPrefix(strings.ToLower(userAgent), githubCopilotUserAgentPrefix) {
		return nil, sdkaccess.ErrNotHandled // Not a GitHub Copilot client, let other providers handle it
	}

	// At this point, we know it's a GitHub Copilot client.
	// We need to verify if an active GitHub Copilot credential exists.

	if globalAuthManager == nil {
		log.Warn("github-copilot access provider: globalAuthManager is not set, cannot authenticate.")
		return nil, sdkaccess.ErrNoCredentials
	}

	// Try to find an active Auth record for "github-copilot"
	auths := globalAuthManager.ListAuthsByProvider(AccessProviderTypeGitHubCopilot)
	if len(auths) == 0 {
		log.Debugf("github-copilot access provider: no active auth records found for %s", AccessProviderTypeGitHubCopilot)
		return nil, sdkaccess.ErrNoCredentials // No configured GitHub Copilot accounts
	}

	// For now, just pick the first available active GitHub Copilot auth.
	// A more sophisticated implementation might use a picker (like round-robin or fill-first)
	// from coreauth.Manager.
	var activeAuth *coreauth.Auth
	for _, auth := range auths {
		if !auth.Disabled && auth.Status == coreauth.StatusActive {
			activeAuth = auth
			break
		}
	}

	if activeAuth == nil {
		log.Debug("github-copilot access provider: no active github-copilot auth record found")
		return nil, sdkaccess.ErrNoCredentials
	}

	// Return a successful result. The actual token will be retrieved by the executor.
	return &sdkaccess.Result{
		Provider:  p.Identifier(),
		Principal: activeAuth.Label, // Use username or ID as principal
		Metadata: map[string]string{
			"source": "github-copilot-access-provider",
			"auth_id": activeAuth.ID,
		},
	}, nil
}
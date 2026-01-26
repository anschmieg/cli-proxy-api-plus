package github_copilot_access

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth" // Keep this import for now, as it's used elsewhere in the package scope
	log "github.com/sirupsen/logrus"
)

const (
	AccessProviderTypeGitHubCopilot = "github-copilot"
	githubCopilotUserAgentPrefix = "github-copilot-cli"
)

var (
	registerOnce sync.Once
	// globalAuthManager *coreauth.Manager // Removed for now
)

// SetGlobalAuthManager allows setting the global coreauth.Manager for this package.
// We will comment this out for now since globalAuthManager is removed.
// func SetGlobalAuthManager(mgr *coreauth.Manager) {
// 	globalAuthManager = mgr
// }

// Register ensures the GitHub Copilot access provider is available to the access manager.
func Register() {
	registerOnce.Do(func() {
		sdkaccess.RegisterProvider(AccessProviderTypeGitHubCopilot, newProvider)
		log.Debugf("Registered GitHub Copilot access provider.")
	})
}

// provider implements the sdkaccess.Provider interface for GitHub Copilot.
type provider struct {
	cfg *config.Config
}

func newProvider(cfg *sdkconfig.AccessProvider, sdkCfg *sdkconfig.SDKConfig) (sdkaccess.Provider, error) {
	appCfg := &config.Config{SDKConfig: *sdkCfg}
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

	// For now, if User-Agent matches, we just signal that this provider *could* handle it.
	// We are intentionally NOT looking up auths here to simplify and isolate the build issue.
	log.Debug("github-copilot access provider: User-Agent matched, but returning ErrNotHandled for build test.")
	return nil, sdkaccess.ErrNotHandled // Signal that we are this provider type, but not fully authenticating yet.

	// The original logic is commented out to simplify for build testing:
	/*
	if globalAuthManager == nil {
		log.Warn("github-copilot access provider: globalAuthManager is not set, cannot authenticate.")
		return nil, sdkaccess.ErrNoCredentials
	}

	var activeAuth *coreauth.Auth
	auths := globalAuthManager.List()
	for _, auth := range auths {
		if auth.Provider == AccessProviderTypeGitHubCopilot && !auth.Disabled && auth.Status == coreauth.StatusActive {
			activeAuth = auth
			break
		}
	}

	if activeAuth == nil {
		log.Debug("github-copilot access provider: no active github-copilot auth record found")
		return nil, sdkaccess.ErrNoCredentials
	}

	return &sdkaccess.Result{
		Provider:  p.Identifier(),
		Principal: activeAuth.Label,
		Metadata: map[string]string{
			"source": "github-copilot-access-provider",
			"auth_id": activeAuth.ID,
		},
	}, nil
	*/
}

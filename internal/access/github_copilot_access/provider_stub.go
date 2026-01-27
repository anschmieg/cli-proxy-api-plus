package github_copilot_access

import coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"

// Minimal compile-time stub so builds succeed when the full provider
// implementation is not present in the build context.
var globalAuthManager *coreauth.Manager

func Register() {}

func SetGlobalAuthManager(mgr *coreauth.Manager) {
	globalAuthManager = mgr
}


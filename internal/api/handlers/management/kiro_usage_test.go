package management

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestGetKiroQuotaStatus_EmptyAuthDir(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	cfg := &config.Config{
		AuthDir: authDir,
	}
	h := NewHandler(cfg, filepath.Join(authDir, "config.yaml"), nil)

	req := httptest.NewRequest(http.MethodGet, "/v0/management/kiro/quota", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	h.GetKiroQuotaStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	body := w.Body.String()
	if body == "" || body == "{}" {
		t.Fatalf("expected non-empty JSON response, got %q", body)
	}
	if want := "\"accounts\":[]"; !contains(body, want) {
		t.Fatalf("expected response to contain %s, got %s", want, body)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestGetKiroQuotaStatus_AuthDirMissing(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	missingDir := filepath.Join(t.TempDir(), "missing")
	_ = os.RemoveAll(missingDir)
	cfg := &config.Config{
		AuthDir: missingDir,
	}
	h := NewHandler(cfg, filepath.Join(missingDir, "config.yaml"), nil)

	req := httptest.NewRequest(http.MethodGet, "/v0/management/kiro/quota", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	h.GetKiroQuotaStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for missing auth dir, got %d", w.Code)
	}
}

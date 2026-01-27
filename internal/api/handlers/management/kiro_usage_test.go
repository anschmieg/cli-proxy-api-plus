package management

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	kiroauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/kiro"
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
	if want := "\"accounts\":[]"; !contains(w.Body.String(), want) {
		t.Fatalf("expected response to contain %s, got %s", want, w.Body.String())
	}
}

func TestGetKiroQuotaStatus_AuthDirIsFile(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	authFile := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(authFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to create auth file: %v", err)
	}

	cfg := &config.Config{
		AuthDir: authFile,
	}
	h := NewHandler(cfg, filepath.Join(tmpDir, "config.yaml"), nil)

	req := httptest.NewRequest(http.MethodGet, "/v0/management/kiro/quota", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	h.GetKiroQuotaStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for auth-dir file, got %d", w.Code)
	}
	if want := "\"accounts\":[]"; !contains(w.Body.String(), want) {
		t.Fatalf("expected response to contain %s, got %s", want, w.Body.String())
	}
}

func TestFormatEpochMillis(t *testing.T) {
	t.Parallel()

	if got := formatEpochMillis(0); got != "" {
		t.Fatalf("expected empty string for zero epoch, got %q", got)
	}

	ts := time.Date(2026, time.January, 27, 13, 0, 0, 0, time.UTC)
	want := ts.Format(time.RFC3339)

	if got := formatEpochMillis(float64(ts.UnixMilli())); got != want {
		t.Fatalf("expected millis %q, got %q", want, got)
	}

	if got := formatEpochMillis(float64(ts.Unix())); got != want {
		t.Fatalf("expected seconds %q, got %q", want, got)
	}
}

func TestEpochSecondsFromUpstream(t *testing.T) {
	t.Parallel()

	seconds, ok := epochSecondsFromUpstream(0)
	if ok || seconds != 0 {
		t.Fatalf("expected zero/false for 0 input, got %d/%v", seconds, ok)
	}

	ts := time.Date(2026, time.January, 27, 13, 0, 0, 0, time.UTC)
	want := ts.Unix()

	if got, ok := epochSecondsFromUpstream(float64(ts.Unix())); !ok || got != want {
		t.Fatalf("expected seconds %d, got %d (ok=%v)", want, got, ok)
	}

	if got, ok := epochSecondsFromUpstream(float64(ts.UnixMilli())); !ok || got != want {
		t.Fatalf("expected millis -> seconds %d, got %d (ok=%v)", want, got, ok)
	}
}

func TestAggregateUsage_UsesPrecisionAndInts(t *testing.T) {
	t.Parallel()

	breakdowns := []kiroauth.UsageBreakdown{
		{
			CurrentUsageWithPrecision: floatPtr(1.5),
			UsageLimitWithPrecision:   floatPtr(10),
		},
		{
			CurrentUsage: intPtr(2),
			UsageLimit:   intPtr(5),
		},
		{
			CurrentUsageWithPrecision: floatPtr(3),
			UsageLimitWithPrecision:   floatPtr(0),
		},
	}

	current, limit := aggregateUsage(breakdowns)
	if current != 3.5 || limit != 15 {
		t.Fatalf("expected current=3.5 limit=15, got current=%v limit=%v", current, limit)
	}
}

func TestChooseResetEpochSeconds_PrefersEarliestFuture(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 27, 13, 0, 0, 0, time.UTC)
	past := float64(now.Add(-time.Hour).Unix())
	futureLate := float64(now.Add(2 * time.Hour).Unix())
	futureEarlyMillis := float64(now.Add(30 * time.Minute).UnixMilli())

	breakdowns := []kiroauth.UsageBreakdown{
		{NextDateReset: &past},
		{NextDateReset: &futureLate},
		{NextDateReset: &futureEarlyMillis},
	}

	got := chooseResetEpochSeconds(breakdowns, nil, now)
	want := now.Add(30 * time.Minute).Unix()
	if got != want {
		t.Fatalf("expected earliest future reset %d, got %d", want, got)
	}
}

func TestUsagePercent_Safe(t *testing.T) {
	t.Parallel()

	if got := usagePercent(1, 0); got != 0 {
		t.Fatalf("expected 0 percent for zero limit, got %v", got)
	}

	if got := usagePercent(math.Inf(1), 1); got != 0 {
		t.Fatalf("expected 0 percent for infinite usage, got %v", got)
	}

	if got := usagePercent(5, 20); got != 25 {
		t.Fatalf("expected 25 percent, got %v", got)
	}
}

func floatPtr(v float64) *float64 {
	return &v
}

func intPtr(v int) *int {
	return &v
}

func TestKiroQuotaCache(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	now := time.Date(2026, time.January, 27, 16, 0, 0, 0, time.UTC)
	accounts := []kiroQuotaEntry{{ID: "alpha"}}

	h.setCachedKiroQuota("dir-a", now, accounts)

	if cached, ok := h.getCachedKiroQuota("dir-a", now.Add(10*time.Second)); !ok || len(cached) != 1 {
		t.Fatalf("expected cached result, got ok=%v len=%d", ok, len(cached))
	}

	// Ensure we return a defensive copy.
	cached, ok := h.getCachedKiroQuota("dir-a", now.Add(10*time.Second))
	if !ok {
		t.Fatal("expected cached result on second read")
	}
	cached[0].ID = "mutated"
	cachedAgain, ok := h.getCachedKiroQuota("dir-a", now.Add(10*time.Second))
	if !ok {
		t.Fatal("expected cached result on third read")
	}
	if cachedAgain[0].ID != "alpha" {
		t.Fatalf("expected defensive copy, got %q", cachedAgain[0].ID)
	}

	if _, ok := h.getCachedKiroQuota("dir-b", now.Add(10*time.Second)); ok {
		t.Fatal("expected cache miss for different auth dir")
	}

	if _, ok := h.getCachedKiroQuota("dir-a", now.Add(kiroQuotaCacheTTL+time.Second)); ok {
		t.Fatal("expected cache miss after ttl")
	}
}

package pool

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestNewResolver_Nil(t *testing.T) {
	r := NewResolver(nil)
	if r == nil {
		t.Fatal("expected non-nil resolver")
	}
	res := r.Resolve("anything")
	if res.Matched {
		t.Error("expected no match for nil config")
	}
}

func TestNewResolver_Disabled(t *testing.T) {
	r := NewResolver(&config.ModelPoolConfig{
		Enabled: false,
		Pools: []config.ModelPool{
			{ID: "test-pool", Members: []config.PoolMember{{Model: "m1"}}},
		},
	})
	res := r.Resolve("test-pool")
	if res.Matched {
		t.Error("expected no match when disabled")
	}
}

func TestResolvePool_Basic(t *testing.T) {
	r := NewResolver(&config.ModelPoolConfig{
		Enabled: true,
		Pools: []config.ModelPool{
			{
				ID: "claude-sonnet-4",
				Members: []config.PoolMember{
					{Model: "kiro-claude-sonnet-4-5", Provider: "kiro", Priority: 0, Weight: 2},
					{Model: "claude-sonnet-4-20250514", Provider: "claude", Priority: 1},
				},
				Strategy: "priority",
			},
		},
	})

	res := r.Resolve("claude-sonnet-4")
	if !res.Matched {
		t.Fatal("expected match")
	}
	if res.Strategy != "priority" {
		t.Errorf("expected strategy 'priority', got %q", res.Strategy)
	}
	if len(res.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(res.Targets))
	}
	if res.Targets[0].Model != "kiro-claude-sonnet-4-5" {
		t.Errorf("expected first target model 'kiro-claude-sonnet-4-5', got %q", res.Targets[0].Model)
	}
	if res.Targets[0].Weight != 2 {
		t.Errorf("expected weight 2, got %d", res.Targets[0].Weight)
	}
	if res.Targets[1].Priority != 1 {
		t.Errorf("expected priority 1, got %d", res.Targets[1].Priority)
	}
}

func TestResolvePool_CaseInsensitive(t *testing.T) {
	r := NewResolver(&config.ModelPoolConfig{
		Enabled: true,
		Pools: []config.ModelPool{
			{ID: "My-Pool", Members: []config.PoolMember{{Model: "m1"}}},
		},
	})
	res := r.Resolve("my-pool")
	if !res.Matched {
		t.Error("expected case-insensitive match")
	}
}

func TestResolvePool_DefaultWeight(t *testing.T) {
	r := NewResolver(&config.ModelPoolConfig{
		Enabled: true,
		Pools: []config.ModelPool{
			{ID: "pool", Members: []config.PoolMember{{Model: "m1"}}},
		},
	})
	res := r.Resolve("pool")
	if !res.Matched {
		t.Fatal("expected match")
	}
	if res.Targets[0].Weight != 1 {
		t.Errorf("expected default weight 1, got %d", res.Targets[0].Weight)
	}
}

func TestResolveCluster_Basic(t *testing.T) {
	r := NewResolver(&config.ModelPoolConfig{
		Enabled: true,
		Pools: []config.ModelPool{
			{
				ID: "claude-sonnet",
				Members: []config.PoolMember{
					{Model: "kiro-claude-sonnet-4-5", Provider: "kiro"},
				},
			},
			{
				ID: "gpt-4.1",
				Members: []config.PoolMember{
					{Model: "gpt-4.1", Provider: "codex"},
				},
			},
		},
		Clusters: []config.ModelCluster{
			{
				ID:          "coding-high",
				Description: "High-quality coding models",
				Members: []config.ClusterMember{
					{Pool: "claude-sonnet", Priority: 0},
					{Pool: "gpt-4.1", Priority: 1},
				},
			},
		},
	})

	res := r.Resolve("coding-high")
	if !res.Matched {
		t.Fatal("expected match")
	}
	if res.Strategy != "priority" {
		t.Errorf("expected default cluster strategy 'priority', got %q", res.Strategy)
	}
	if len(res.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(res.Targets))
	}
	if res.Targets[0].Model != "kiro-claude-sonnet-4-5" {
		t.Errorf("expected first target 'kiro-claude-sonnet-4-5', got %q", res.Targets[0].Model)
	}
}

func TestResolveCluster_DirectModel(t *testing.T) {
	r := NewResolver(&config.ModelPoolConfig{
		Enabled: true,
		Clusters: []config.ModelCluster{
			{
				ID: "fast",
				Members: []config.ClusterMember{
					{Model: "gpt-4.1-mini"},
				},
			},
		},
	})
	res := r.Resolve("fast")
	if !res.Matched {
		t.Fatal("expected match")
	}
	if res.Targets[0].Model != "gpt-4.1-mini" {
		t.Errorf("unexpected model: %q", res.Targets[0].Model)
	}
}

func TestIsPoolOrCluster(t *testing.T) {
	r := NewResolver(&config.ModelPoolConfig{
		Enabled: true,
		Pools:   []config.ModelPool{{ID: "p1", Members: []config.PoolMember{{Model: "m1"}}}},
		Clusters: []config.ModelCluster{{ID: "c1", Members: []config.ClusterMember{{Pool: "p1"}}}},
	})
	if !r.IsPoolOrCluster("p1") {
		t.Error("expected pool match")
	}
	if !r.IsPoolOrCluster("c1") {
		t.Error("expected cluster match")
	}
	if r.IsPoolOrCluster("unknown") {
		t.Error("expected no match for unknown")
	}
}

func TestListVirtualModels(t *testing.T) {
	r := NewResolver(&config.ModelPoolConfig{
		Enabled: true,
		Pools:   []config.ModelPool{{ID: "p1", Members: []config.PoolMember{{Model: "m1"}}}},
		Clusters: []config.ModelCluster{{ID: "c1", Description: "desc", Members: []config.ClusterMember{{Pool: "p1"}}}},
	})
	models := r.ListVirtualModels()
	if len(models) != 2 {
		t.Fatalf("expected 2 virtual models, got %d", len(models))
	}
	foundPool, foundCluster := false, false
	for _, m := range models {
		if m.ID == "p1" && m.Type == "pool" {
			foundPool = true
		}
		if m.ID == "c1" && m.Type == "cluster" && m.Description == "desc" {
			foundCluster = true
		}
	}
	if !foundPool {
		t.Error("missing pool in virtual models")
	}
	if !foundCluster {
		t.Error("missing cluster in virtual models")
	}
}

func TestReload(t *testing.T) {
	r := NewResolver(&config.ModelPoolConfig{Enabled: true, Pools: []config.ModelPool{{ID: "a"}}})
	if !r.IsPoolOrCluster("a") {
		t.Fatal("expected 'a'")
	}
	r.Reload(&config.ModelPoolConfig{Enabled: true, Pools: []config.ModelPool{{ID: "b"}}})
	if r.IsPoolOrCluster("a") {
		t.Error("'a' should be gone after reload")
	}
	if !r.IsPoolOrCluster("b") {
		t.Error("expected 'b' after reload")
	}
}

func TestPoolIDFallback(t *testing.T) {
	r := NewResolver(&config.ModelPoolConfig{
		Enabled: true,
		Pools: []config.ModelPool{
			{Members: []config.PoolMember{{Model: "fallback-model"}}},
		},
	})
	res := r.Resolve("fallback-model")
	if !res.Matched {
		t.Error("expected match using first member model as ID")
	}
}

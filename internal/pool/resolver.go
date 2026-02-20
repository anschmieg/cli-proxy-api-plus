// Package pool implements model pooling and clustering for the AI gateway.
// It provides a two-level resolution system:
//   - Level 1 (Pools): aggregate the same model from different providers under one virtual ID.
//   - Level 2 (Clusters): group multiple pools/models under a semantic name (e.g., "coding-high").
//
// The Resolver is built from config and used during request routing to expand
// virtual model IDs into concrete model+provider pairs.
package pool

import (
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
)

// ResolvedTarget is a concrete model+provider pair produced by pool/cluster resolution.
type ResolvedTarget struct {
	// Model is the actual model ID to send to the provider.
	Model string
	// Providers lists provider types that can serve this model. Empty means "all registered providers".
	Providers []string
	// Priority is the selection priority (lower = preferred).
	Priority int
	// Weight for weighted selection.
	Weight int
}

// Resolution is the result of resolving a model ID through the pool/cluster system.
type Resolution struct {
	// Matched is true if the model ID was a pool or cluster ID.
	Matched bool
	// Targets are the concrete model+provider pairs, ordered by priority/config order.
	Targets []ResolvedTarget
	// Strategy is the selection strategy for this resolution.
	Strategy string
}

// Resolver resolves virtual model IDs (pools/clusters) to concrete model+provider pairs.
type Resolver struct {
	mu       sync.RWMutex
	pools    map[string]*config.ModelPool
	clusters map[string]*config.ModelCluster
	strategy string
	enabled  bool
}

// NewResolver creates a Resolver from the given config.
func NewResolver(cfg *config.ModelPoolConfig) *Resolver {
	r := &Resolver{
		pools:    make(map[string]*config.ModelPool),
		clusters: make(map[string]*config.ModelCluster),
	}
	if cfg == nil {
		return r
	}
	r.enabled = cfg.Enabled
	r.strategy = cfg.DefaultStrategy
	if r.strategy == "" {
		r.strategy = "round-robin"
	}
	for i := range cfg.Pools {
		p := &cfg.Pools[i]
		id := strings.TrimSpace(p.ID)
		if id == "" && len(p.Members) > 0 {
			id = p.Members[0].Model
		}
		if id != "" {
			r.pools[strings.ToLower(id)] = p
		}
	}
	for i := range cfg.Clusters {
		c := &cfg.Clusters[i]
		id := strings.TrimSpace(c.ID)
		if id != "" {
			r.clusters[strings.ToLower(id)] = c
		}
	}
	return r
}

// Reload replaces the resolver's config. Thread-safe.
func (r *Resolver) Reload(cfg *config.ModelPoolConfig) {
	fresh := NewResolver(cfg)
	r.mu.Lock()
	r.pools = fresh.pools
	r.clusters = fresh.clusters
	r.strategy = fresh.strategy
	r.enabled = fresh.enabled
	r.mu.Unlock()
}

// Resolve attempts to resolve a model ID as a pool or cluster.
// Returns a Resolution with Matched=false if the ID is not a pool or cluster.
func (r *Resolver) Resolve(modelID string) Resolution {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.enabled {
		return Resolution{}
	}

	key := strings.ToLower(strings.TrimSpace(modelID))

	// Try cluster first (Level 2)
	if cluster, ok := r.clusters[key]; ok {
		return r.resolveCluster(cluster)
	}

	// Try pool (Level 1)
	if pool, ok := r.pools[key]; ok {
		return r.resolvePool(pool)
	}

	return Resolution{}
}

// IsPoolOrCluster checks if a model ID is a known pool or cluster without full resolution.
func (r *Resolver) IsPoolOrCluster(modelID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.enabled {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(modelID))
	if _, ok := r.pools[key]; ok {
		return true
	}
	if _, ok := r.clusters[key]; ok {
		return true
	}
	return false
}

// ListVirtualModels returns all pool and cluster IDs that should appear in model listings.
func (r *Resolver) ListVirtualModels() []VirtualModel {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.enabled {
		return nil
	}

	var models []VirtualModel
	for _, p := range r.pools {
		id := p.ID
		if id == "" && len(p.Members) > 0 {
			id = p.Members[0].Model
		}
		if id != "" {
			models = append(models, VirtualModel{
				ID:   id,
				Type: "pool",
			})
		}
	}
	for _, c := range r.clusters {
		if c.ID != "" {
			models = append(models, VirtualModel{
				ID:          c.ID,
				Type:        "cluster",
				Description: c.Description,
			})
		}
	}
	return models
}

// VirtualModel describes a pool or cluster for model listing purposes.
type VirtualModel struct {
	ID          string
	Type        string // "pool" or "cluster"
	Description string
}

func (r *Resolver) resolvePool(pool *config.ModelPool) Resolution {
	strategy := pool.Strategy
	if strategy == "" {
		strategy = r.strategy
	}

	var targets []ResolvedTarget
	for _, m := range pool.Members {
		weight := m.Weight
		if weight <= 0 {
			weight = 1
		}
		providers := r.resolveProviders(m)
		targets = append(targets, ResolvedTarget{
			Model:     m.Model,
			Providers: providers,
			Priority:  m.Priority,
			Weight:    weight,
		})
	}

	return Resolution{
		Matched:  true,
		Targets:  targets,
		Strategy: strategy,
	}
}

func (r *Resolver) resolveCluster(cluster *config.ModelCluster) Resolution {
	strategy := cluster.Strategy
	if strategy == "" {
		strategy = "priority"
	}

	var targets []ResolvedTarget
	for _, member := range cluster.Members {
		if member.Pool != "" {
			poolKey := strings.ToLower(strings.TrimSpace(member.Pool))
			if pool, ok := r.pools[poolKey]; ok {
				for _, m := range pool.Members {
					weight := m.Weight
					if weight <= 0 {
						weight = 1
					}
					providers := r.resolveProviders(m)
					targets = append(targets, ResolvedTarget{
						Model:     m.Model,
						Providers: providers,
						Priority:  member.Priority + m.Priority,
						Weight:    weight,
					})
				}
			}
		} else if member.Model != "" {
			providers := registry.GetGlobalRegistry().GetModelProviders(member.Model)
			targets = append(targets, ResolvedTarget{
				Model:     member.Model,
				Providers: providers,
				Priority:  member.Priority,
				Weight:    1,
			})
		}
	}

	return Resolution{
		Matched:  true,
		Targets:  targets,
		Strategy: strategy,
	}
}

func (r *Resolver) resolveProviders(m config.PoolMember) []string {
	if m.Provider != "" {
		return []string{m.Provider}
	}
	// Look up all providers from the model registry
	return registry.GetGlobalRegistry().GetModelProviders(m.Model)
}

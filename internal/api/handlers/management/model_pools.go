package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// GetModelPools returns the current model-pools configuration.
func (h *Handler) GetModelPools(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.JSON(http.StatusOK, h.cfg.ModelPools)
}

// PutModelPools replaces the entire model-pools configuration.
func (h *Handler) PutModelPools(c *gin.Context) {
	var pools config.ModelPoolConfig
	if err := c.ShouldBindJSON(&pools); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.mu.Lock()
	h.cfg.ModelPools = pools
	h.mu.Unlock()

	h.persist(c)
}

// PatchModelPool adds or updates a single pool by ID.
func (h *Handler) PatchModelPool(c *gin.Context) {
	var pool config.ModelPool
	if err := c.ShouldBindJSON(&pool); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if pool.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pool id is required"})
		return
	}

	h.mu.Lock()
	found := false
	for i, p := range h.cfg.ModelPools.Pools {
		if p.ID == pool.ID {
			h.cfg.ModelPools.Pools[i] = pool
			found = true
			break
		}
	}
	if !found {
		h.cfg.ModelPools.Pools = append(h.cfg.ModelPools.Pools, pool)
	}
	h.mu.Unlock()

	h.persist(c)
}

// DeleteModelPool removes a pool by ID.
func (h *Handler) DeleteModelPool(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id query parameter is required"})
		return
	}

	h.mu.Lock()
	pools := h.cfg.ModelPools.Pools
	found := false
	for i, p := range pools {
		if p.ID == id {
			h.cfg.ModelPools.Pools = append(pools[:i], pools[i+1:]...)
			found = true
			break
		}
	}
	h.mu.Unlock()

	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "pool not found"})
		return
	}

	h.persist(c)
}

// PatchModelCluster adds or updates a single cluster by ID.
func (h *Handler) PatchModelCluster(c *gin.Context) {
	var cluster config.ModelCluster
	if err := c.ShouldBindJSON(&cluster); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if cluster.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster id is required"})
		return
	}

	h.mu.Lock()
	found := false
	for i, cl := range h.cfg.ModelPools.Clusters {
		if cl.ID == cluster.ID {
			h.cfg.ModelPools.Clusters[i] = cluster
			found = true
			break
		}
	}
	if !found {
		h.cfg.ModelPools.Clusters = append(h.cfg.ModelPools.Clusters, cluster)
	}
	h.mu.Unlock()

	h.persist(c)
}

// DeleteModelCluster removes a cluster by ID.
func (h *Handler) DeleteModelCluster(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id query parameter is required"})
		return
	}

	h.mu.Lock()
	clusters := h.cfg.ModelPools.Clusters
	found := false
	for i, cl := range clusters {
		if cl.ID == id {
			h.cfg.ModelPools.Clusters = append(clusters[:i], clusters[i+1:]...)
			found = true
			break
		}
	}
	h.mu.Unlock()

	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "cluster not found"})
		return
	}

	h.persist(c)
}

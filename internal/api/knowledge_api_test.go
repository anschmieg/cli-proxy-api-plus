package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/knowledge"
)

type fakeEmbedder struct{}

func (f *fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1.0, 0.0}
	}
	return out, nil
}

func (f *fakeEmbedder) GetModel() string {
	return "fake-embedding-model"
}

func TestKnowledgeFileManagement(t *testing.T) {
	server := newTestServer(t)

	// Inject memory store and fake embedder
	store := knowledge.NewMemoryStore()
	embedder := &fakeEmbedder{}
	manager := knowledge.NewManager(store, embedder)
	server.handlers.UpdateKnowledgeManager(manager)

	// Enable management routes for testing
	server.cfg.RemoteManagement.AllowRemote = true
	server.cfg.RemoteManagement.SecretKey = "$2a$10$9qUj09M4DTvbyLw.s7/zVO3TnYaf3s4Vfd3hg6hwlStKGI/ftbPr2"
	server.managementRoutesEnabled.Store(true)
	server.registerManagementRoutes()

	// Seed some data
	ctx := context.Background()
	_ = manager.AddText(ctx, "doc-1", "content 1", map[string]any{
		"project":  "proj-1",
		"fileId":   "file-1",
		"filename": "file1.txt",
	})
	_ = manager.AddText(ctx, "doc-2", "content 2", map[string]any{
		"project":  "proj-1",
		"fileId":   "file-2",
		"filename": "file2.txt",
	})
	_ = manager.AddText(ctx, "doc-3", "content 3", map[string]any{
		"project":  "proj-2",
		"fileId":   "file-3",
		"filename": "file3.txt",
	})

	t.Run("ListProjects", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/management/knowledge/projects", nil)
		req.Header.Set("Authorization", "Bearer test-key")
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d, body: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Projects []string `json:"projects"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Projects) != 2 {
			t.Fatalf("expected 2 projects, got %v", resp.Projects)
		}
	})

	t.Run("ListFiles", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/management/knowledge/projects/proj-1/files", nil)
		req.Header.Set("Authorization", "Bearer test-key")
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d", rr.Code)
		}
		var resp struct {
			Files []knowledge.FileDescriptor `json:"files"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Files) != 2 {
			t.Fatalf("expected 2 files, got %d", len(resp.Files))
		}
	})

	t.Run("DeleteFile", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/v0/management/knowledge/projects/proj-1/files/file-1", nil)
		req.Header.Set("Authorization", "Bearer test-key")
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d", rr.Code)
		}

		// Verify deletion
		files, _ := manager.ListFiles(ctx, "proj-1", 100)
		if len(files) != 1 || files[0].FileID != "file-2" {
			t.Fatalf("expected 1 file remaining (file-2), got %v", files)
		}
	})

	t.Run("DeleteProject", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/v0/management/knowledge/projects/proj-2", nil)
		req.Header.Set("Authorization", "Bearer test-key")
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d", rr.Code)
		}

		// Verify deletion
		projects, _ := manager.ListProjects(ctx, 100)
		if len(projects) != 1 || projects[0] != "proj-1" {
			t.Fatalf("expected 1 project remaining (proj-1), got %v", projects)
		}
	})
}

package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestQdrantStore_SearchAndAutoCreate(t *testing.T) {
	t.Parallel()
	if os.Getenv("ALLOW_HTTP_TESTS") == "" {
		t.Skip("http listener not allowed in this environment")
	}

	var createCalled bool
	var searchCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/collections/test":
			if r.Method == http.MethodGet {
				if !createCalled {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.Method == http.MethodPut {
				createCalled = true
				w.WriteHeader(http.StatusOK)
				return
			}
		case "/collections/test/points/search":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			searchCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{
					{
						"id":    "doc-1",
						"score": 0.9,
						"payload": map[string]any{
							"content":  "hello",
							"filename": "file.txt",
						},
					},
				},
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantOptions{
		BaseURL:    server.URL,
		Collection: "test",
		VectorSize: 2,
		Distance:   "Cosine",
		AutoCreate: true,
	})
	if store == nil {
		t.Fatal("expected store")
	}

	docs, err := store.Search(context.Background(), []float32{0.1, 0.2}, 2, map[string]string{"project": "proj"})
	if err != nil {
		t.Fatalf("search error: %v", err)
	}
	if !createCalled {
		t.Fatalf("expected collection create")
	}
	if !searchCalled {
		t.Fatalf("expected search call")
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Content != "hello" {
		t.Fatalf("unexpected content")
	}
	if docs[0].Score <= 0 {
		t.Fatalf("expected score set")
	}
}

package knowledge

import (
	"context"
	"fmt"
)

// Document represents a piece of knowledge.
type Document struct {
	ID       string                 `json:"id"`
	Content  string                 `json:"content"`
	Metadata map[string]interface{} `json:"metadata"`
	Vector   []float32              `json:"vector,omitempty"`
	Score    float32                `json:"score,omitempty"`
}

// Store defines the interface for a knowledge base storage.
type Store interface {
	// Add adds documents to the store.
	Add(ctx context.Context, docs []Document) error
	// Search finds relevant documents for a query vector.
	Search(ctx context.Context, vector []float32, limit int, filter map[string]string) ([]Document, error)
	// Delete removes a document by ID.
	Delete(ctx context.Context, id string) error
}

// FileDescriptor summarizes a stored file within a knowledge base.
type FileDescriptor struct {
	FileID   string `json:"fileId"`
	Filename string `json:"filename"`
}

// FileStore exposes file-centric operations for knowledge backends that support them.
type FileStore interface {
	ListProjects(ctx context.Context, limit int) ([]string, error)
	ListFiles(ctx context.Context, project string, limit int) ([]FileDescriptor, error)
	DeleteByFilter(ctx context.Context, filter map[string]string) error
}

// Embedder defines the interface for converting text to vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	GetModel() string
}

// Manager orchestrates the knowledge base operations.
type Manager struct {
	store    Store
	embedder Embedder
}

func NewManager(store Store, embedder Embedder) *Manager {
	return &Manager{
		store:    store,
		embedder: embedder,
	}
}

func (m *Manager) AddText(ctx context.Context, id string, text string, metadata map[string]interface{}) error {
	vectors, err := m.embedder.Embed(ctx, []string{text})
	if err != nil {
		return err
	}
	if len(vectors) == 0 {
		return nil
	}

	doc := Document{
		ID:       id,
		Content:  text,
		Metadata: metadata,
		Vector:   vectors[0],
	}
	return m.store.Add(ctx, []Document{doc})
}

func (m *Manager) GetEmbedder() Embedder {
	return m.embedder
}

func (m *Manager) AddDocuments(ctx context.Context, docs []Document) error {
	if m == nil || m.embedder == nil || m.store == nil {
		return fmt.Errorf("knowledge manager not configured")
	}
	if len(docs) == 0 {
		return nil
	}

	texts := make([]string, len(docs))
	for i, doc := range docs {
		texts[i] = doc.Content
	}

	vectors, err := m.embedder.Embed(ctx, texts)
	if err != nil {
		return err
	}
	if len(vectors) != len(docs) {
		return fmt.Errorf("embedding count mismatch: got %d vectors for %d docs", len(vectors), len(docs))
	}

	embedded := make([]Document, len(docs))
	for i, doc := range docs {
		doc.Vector = vectors[i]
		embedded[i] = doc
	}

	return m.store.Add(ctx, embedded)
}

func (m *Manager) Query(ctx context.Context, query string, limit int, filter map[string]string) ([]Document, error) {
	vectors, err := m.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, nil
	}

	return m.store.Search(ctx, vectors[0], limit, filter)
}

func (m *Manager) ListProjects(ctx context.Context, limit int) ([]string, error) {
	if m == nil || m.store == nil {
		return nil, fmt.Errorf("knowledge manager not configured")
	}
	store, ok := m.store.(FileStore)
	if !ok {
		return nil, fmt.Errorf("knowledge store does not support project listing")
	}
	return store.ListProjects(ctx, limit)
}

func (m *Manager) ListFiles(ctx context.Context, project string, limit int) ([]FileDescriptor, error) {
	if m == nil || m.store == nil {
		return nil, fmt.Errorf("knowledge manager not configured")
	}
	store, ok := m.store.(FileStore)
	if !ok {
		return nil, fmt.Errorf("knowledge store does not support file listing")
	}
	return store.ListFiles(ctx, project, limit)
}

func (m *Manager) DeleteByFilter(ctx context.Context, filter map[string]string) error {
	if m == nil || m.store == nil {
		return fmt.Errorf("knowledge manager not configured")
	}
	store, ok := m.store.(FileStore)
	if !ok {
		return fmt.Errorf("knowledge store does not support filtered deletes")
	}
	return store.DeleteByFilter(ctx, filter)
}

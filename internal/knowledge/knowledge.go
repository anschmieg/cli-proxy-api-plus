package knowledge

import "context"

// Document represents a piece of knowledge.
type Document struct {
	ID       string                 `json:"id"`
	Content  string                 `json:"content"`
	Metadata map[string]interface{} `json:"metadata"`
	Vector   []float32              `json:"vector,omitempty"`
}

// Store defines the interface for a knowledge base storage.
type Store interface {
	// Add adds documents to the store.
	Add(ctx context.Context, docs []Document) error
	// Search finds relevant documents for a query vector.
	Search(ctx context.Context, vector []float32, limit int) ([]Document, error)
	// Delete removes a document by ID.
	Delete(ctx context.Context, id string) error
}

// Embedder defines the interface for converting text to vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
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

func (m *Manager) Query(ctx context.Context, query string, limit int) ([]Document, error) {
	vectors, err := m.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, nil
	}
	
	return m.store.Search(ctx, vectors[0], limit)
}

package knowledge

import (
	"context"
	"math"
	"sort"
	"sync"
)

type MemoryStore struct {
	docs map[string]Document
	mu   sync.RWMutex
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		docs: make(map[string]Document),
	}
}

func (s *MemoryStore) Add(ctx context.Context, docs []Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, doc := range docs {
		s.docs[doc.ID] = doc
	}
	return nil
}

func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.docs, id)
	return nil
}

func (s *MemoryStore) ListProjects(ctx context.Context, limit int) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	projects := make(map[string]struct{})
	for _, doc := range s.docs {
		if project, ok := doc.Metadata["project"].(string); ok && project != "" {
			projects[project] = struct{}{}
		}
	}

	out := make([]string, 0, len(projects))
	for project := range projects {
		out = append(out, project)
	}
	sort.Strings(out)

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *MemoryStore) ListFiles(ctx context.Context, project string, limit int) ([]FileDescriptor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files := make(map[string]FileDescriptor)
	for _, doc := range s.docs {
		p, ok := doc.Metadata["project"].(string)
		if !ok || p != project {
			continue
		}

		fileID, _ := doc.Metadata["fileId"].(string)
		filename, _ := doc.Metadata["filename"].(string)
		if fileID != "" {
			files[fileID] = FileDescriptor{
				FileID:   fileID,
				Filename: filename,
			}
		}
	}

	out := make([]FileDescriptor, 0, len(files))
	for _, desc := range files {
		out = append(out, desc)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].FileID < out[j].FileID
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *MemoryStore) DeleteByFilter(ctx context.Context, filter map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, doc := range s.docs {
		if matchesFilter(doc.Metadata, filter) {
			delete(s.docs, id)
		}
	}
	return nil
}

func (s *MemoryStore) Search(ctx context.Context, vector []float32, limit int, filter map[string]string) ([]Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type result struct {
		doc   Document
		score float32
	}

	results := make([]result, 0, len(s.docs))

	for _, doc := range s.docs {
		if len(doc.Vector) != len(vector) {
			continue
		}
		if !matchesFilter(doc.Metadata, filter) {
			continue
		}
		score := cosineSimilarity(doc.Vector, vector)
		doc.Score = score
		results = append(results, result{doc: doc, score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	out := make([]Document, len(results))
	for i, r := range results {
		out[i] = r.doc
	}
	return out, nil
}

func matchesFilter(metadata map[string]interface{}, filter map[string]string) bool {
	if len(filter) == 0 {
		return true
	}
	if len(metadata) == 0 {
		return false
	}
	for key, expected := range filter {
		value, ok := metadata[key]
		if !ok {
			return false
		}
		switch typed := value.(type) {
		case string:
			if typed != expected {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func cosineSimilarity(a, b []float32) float32 {
	var dot, magA, magB float32
	for i := 0; i < len(a); i++ {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(magA))) * float32(math.Sqrt(float64(magB))))
}

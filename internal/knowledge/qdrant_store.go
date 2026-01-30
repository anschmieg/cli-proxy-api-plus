package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type QdrantOptions struct {
	BaseURL    string
	Collection string
	VectorSize int
	Distance   string
	AutoCreate bool
	Timeout    time.Duration
}

type QdrantStore struct {
	baseURL    string
	collection string
	vectorSize int
	distance   string
	autoCreate bool
	client     *http.Client
	once       sync.Once
	onceErr    error
}

func NewQdrantStore(opts QdrantOptions) *QdrantStore {
	baseURL := strings.TrimSpace(opts.BaseURL)
	collection := strings.TrimSpace(opts.Collection)
	if baseURL == "" || collection == "" {
		return nil
	}
	if opts.VectorSize <= 0 {
		opts.VectorSize = 1536
	}
	distance := strings.TrimSpace(opts.Distance)
	if distance == "" {
		distance = "Cosine"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	return &QdrantStore{
		baseURL:    strings.TrimRight(baseURL, "/"),
		collection: collection,
		vectorSize: opts.VectorSize,
		distance:   distance,
		autoCreate: opts.AutoCreate,
		client:     &http.Client{Timeout: timeout},
	}
}

func (s *QdrantStore) Add(ctx context.Context, docs []Document) error {
	if s == nil {
		return fmt.Errorf("qdrant store not configured")
	}
	if len(docs) == 0 {
		return nil
	}
	if err := s.ensureCollection(ctx); err != nil {
		return err
	}

	type point struct {
		ID      string                 `json:"id"`
		Vector  []float32              `json:"vector"`
		Payload map[string]interface{} `json:"payload"`
	}
	payload := struct {
		Points []point `json:"points"`
	}{Points: make([]point, 0, len(docs))}

	for _, doc := range docs {
		payload.Points = append(payload.Points, point{
			ID:      doc.ID,
			Vector:  doc.Vector,
			Payload: doc.Metadata,
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.pointsURL(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("qdrant upsert failed: %s", resp.Status)
	}
	return nil
}

func (s *QdrantStore) Search(ctx context.Context, vector []float32, limit int, filter map[string]string) ([]Document, error) {
	if s == nil {
		return nil, fmt.Errorf("qdrant store not configured")
	}
	if err := s.ensureCollection(ctx); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 5
	}

	type matchValue struct {
		Value string `json:"value"`
	}
	type condition struct {
		Key   string     `json:"key"`
		Match matchValue `json:"match"`
	}
	type searchFilter struct {
		Must []condition `json:"must,omitempty"`
	}

	reqPayload := struct {
		Vector      []float32     `json:"vector"`
		Limit       int           `json:"limit"`
		WithPayload bool          `json:"with_payload"`
		WithVector  bool          `json:"with_vector"`
		Filter      *searchFilter `json:"filter,omitempty"`
	}{
		Vector:      vector,
		Limit:       limit,
		WithPayload: true,
		WithVector:  false,
	}

	if len(filter) > 0 {
		conds := make([]condition, 0, len(filter))
		for key, value := range filter {
			if key == "" || value == "" {
				continue
			}
			conds = append(conds, condition{
				Key:   key,
				Match: matchValue{Value: value},
			})
		}
		if len(conds) > 0 {
			reqPayload.Filter = &searchFilter{Must: conds}
		}
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.searchURL(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("qdrant search failed: %s", resp.Status)
	}

	var decoded struct {
		Result []struct {
			ID      interface{}            `json:"id"`
			Score   float32                `json:"score"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}

	out := make([]Document, 0, len(decoded.Result))
	for _, item := range decoded.Result {
		doc := Document{
			ID:       fmt.Sprint(item.ID),
			Metadata: item.Payload,
			Score:    item.Score,
		}
		if content, ok := item.Payload["content"].(string); ok {
			doc.Content = content
		}
		out = append(out, doc)
	}
	return out, nil
}

func (s *QdrantStore) Delete(ctx context.Context, id string) error {
	if s == nil {
		return fmt.Errorf("qdrant store not configured")
	}
	if err := s.ensureCollection(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return nil
	}

	reqPayload := struct {
		Points []string `json:"points"`
	}{
		Points: []string{id},
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.deleteURL(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("qdrant delete failed: %s", resp.Status)
	}
	return nil
}

func (s *QdrantStore) ensureCollection(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("qdrant store not configured")
	}
	s.once.Do(func() {
		s.onceErr = s.ensureCollectionOnce(ctx)
	})
	return s.onceErr
}

func (s *QdrantStore) ensureCollectionOnce(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.collectionURL(), nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("qdrant collection check failed: %s", resp.Status)
	}
	if !s.autoCreate {
		return fmt.Errorf("qdrant collection %s not found", s.collection)
	}

	payload := struct {
		Vectors struct {
			Size     int    `json:"size"`
			Distance string `json:"distance"`
		} `json:"vectors"`
	}{}
	payload.Vectors.Size = s.vectorSize
	payload.Vectors.Distance = s.distance
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	createReq, err := http.NewRequestWithContext(ctx, http.MethodPut, s.collectionURL(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := s.client.Do(createReq)
	if err != nil {
		return err
	}
	defer createResp.Body.Close()
	if createResp.StatusCode/100 != 2 {
		return fmt.Errorf("qdrant create collection failed: %s", createResp.Status)
	}
	return nil
}

func (s *QdrantStore) ListProjects(ctx context.Context, limit int) ([]string, error) {
	if s == nil {
		return nil, fmt.Errorf("qdrant store not configured")
	}
	if err := s.ensureCollection(ctx); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}

	reqPayload := struct {
		Limit       int  `json:"limit"`
		WithPayload bool `json:"with_payload"`
		WithVector  bool `json:"with_vector"`
	}{
		Limit:       1000, // Reasonable limit to find unique projects
		WithPayload: true,
		WithVector:  false,
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.scrollURL(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var decoded struct {
		Result struct {
			Points []struct {
				Payload map[string]interface{} `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}

	projects := make(map[string]struct{})
	for _, p := range decoded.Result.Points {
		if project, ok := p.Payload["project"].(string); ok && project != "" {
			projects[project] = struct{}{}
		}
	}

	out := make([]string, 0, len(projects))
	for p := range projects {
		out = append(out, p)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *QdrantStore) ListFiles(ctx context.Context, project string, limit int) ([]FileDescriptor, error) {
	if s == nil {
		return nil, fmt.Errorf("qdrant store not configured")
	}
	if err := s.ensureCollection(ctx); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}

	type matchValue struct {
		Value string `json:"value"`
	}
	type condition struct {
		Key   string     `json:"key"`
		Match matchValue `json:"match"`
	}
	type scrollFilter struct {
		Must []condition `json:"must,omitempty"`
	}

	reqPayload := struct {
		Limit       int           `json:"limit"`
		WithPayload bool          `json:"with_payload"`
		WithVector  bool          `json:"with_vector"`
		Filter      *scrollFilter `json:"filter,omitempty"`
	}{
		Limit:       1000,
		WithPayload: true,
		WithVector:  false,
		Filter: &scrollFilter{
			Must: []condition{
				{Key: "project", Match: matchValue{Value: project}},
			},
		},
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.scrollURL(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var decoded struct {
		Result struct {
			Points []struct {
				Payload map[string]interface{} `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}

	files := make(map[string]FileDescriptor)
	for _, p := range decoded.Result.Points {
		fileID, _ := p.Payload["fileId"].(string)
		filename, _ := p.Payload["filename"].(string)
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
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *QdrantStore) DeleteByFilter(ctx context.Context, filter map[string]string) error {
	if s == nil {
		return fmt.Errorf("qdrant store not configured")
	}
	if err := s.ensureCollection(ctx); err != nil {
		return err
	}

	type matchValue struct {
		Value string `json:"value"`
	}
	type condition struct {
		Key   string     `json:"key"`
		Match matchValue `json:"match"`
	}
	type deleteFilter struct {
		Must []condition `json:"must,omitempty"`
	}

	reqPayload := struct {
		Filter deleteFilter `json:"filter"`
	}{
		Filter: deleteFilter{Must: make([]condition, 0, len(filter))},
	}

	for k, v := range filter {
		reqPayload.Filter.Must = append(reqPayload.Filter.Must, condition{
			Key:   k,
			Match: matchValue{Value: v},
		})
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.deleteURL(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("qdrant delete by filter failed: %s", resp.Status)
	}
	return nil
}

func (s *QdrantStore) scrollURL() string {
	return fmt.Sprintf("%s/collections/%s/points/scroll", s.baseURL, s.collection)
}

func (s *QdrantStore) collectionURL() string {
	return fmt.Sprintf("%s/collections/%s", s.baseURL, s.collection)
}

func (s *QdrantStore) pointsURL() string {
	return fmt.Sprintf("%s/collections/%s/points", s.baseURL, s.collection)
}

func (s *QdrantStore) searchURL() string {
	return fmt.Sprintf("%s/collections/%s/points/search", s.baseURL, s.collection)
}

func (s *QdrantStore) deleteURL() string {
	return fmt.Sprintf("%s/collections/%s/points/delete", s.baseURL, s.collection)
}

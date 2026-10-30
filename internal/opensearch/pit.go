package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"tabularis-opensearch-plugin-go/internal/query"
)

type Snapshot struct {
	mu            sync.Mutex
	client        *Client
	id            string
	body          map[string]json.RawMessage
	closed        bool
	shards        int
	expectedIndex string
}

func (c *Client) OpenPIT(ctx context.Context, operation query.Request, verifiedUniqueSort bool) (*Snapshot, error) {
	if err := operation.Validate(); err != nil {
		return nil, err
	}
	if !verifiedUniqueSort {
		return nil, errors.New("PIT pagination requires a verified unique sort")
	}
	index := operation.Index()
	if index == "" || strings.ContainsAny(index, "/\\?#%[] ,") || strings.Contains(index, "..") || operation.Method() != http.MethodPost || operation.Path() != "/"+index+"/_search" {
		return nil, errors.New("invalid PIT search operation")
	}
	allowed := false
	for _, pattern := range strings.Split(c.config.IndexPattern, ",") {
		if index == pattern {
			allowed = true
			break
		}
		if strings.Contains(index, "*") {
			continue
		}
		matched, err := path.Match(pattern, index)
		if err != nil {
			return nil, errors.New("invalid index pattern")
		}
		allowed = allowed || matched
	}
	if !allowed {
		return nil, errors.New("index is outside the configured index pattern")
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(operation.Body(), &body); err != nil || body == nil {
		return nil, errors.New("invalid PIT search body")
	}
	var sortFields []json.RawMessage
	if err := json.Unmarshal(body["sort"], &sortFields); err != nil || len(sortFields) == 0 {
		return nil, errors.New("PIT pagination requires an explicit sort array")
	}
	var size int
	if err := json.Unmarshal(body["size"], &size); err != nil || size < 2 || size > 1001 {
		return nil, errors.New("PIT pagination requires a bounded positive page size")
	}
	values := url.Values{"keep_alive": {"1m"}, "allow_partial_pit_creation": {"false"}}
	response, err := c.pitRequest(ctx, http.MethodPost, "/"+url.PathEscape(index)+"/_search/point_in_time", values, nil)
	if err != nil {
		return nil, err
	}
	var created struct {
		ID     string `json:"pit_id"`
		Shards struct {
			Total  int `json:"total"`
			Failed int `json:"failed"`
		} `json:"_shards"`
	}
	if err := json.Unmarshal(response, &created); err != nil || created.ID == "" {
		return nil, errors.New("PIT creation returned an invalid response")
	}
	snapshot := &Snapshot{client: c, id: created.ID, body: body, shards: created.Shards.Total}
	if created.Shards.Failed != 0 {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if closeErr := snapshot.Close(cleanup); closeErr != nil {
			return nil, fmt.Errorf("PIT creation had partial failures; cleanup failed: %w", closeErr)
		}
		return nil, errors.New("PIT creation had partial failures")
	}
	return snapshot, nil
}

func (s *Snapshot) Search(ctx context.Context, after []json.RawMessage) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("PIT snapshot is closed")
	}
	body := make(map[string]json.RawMessage, len(s.body)+2)
	for key, value := range s.body {
		body[key] = value
	}
	pit, err := json.Marshal(map[string]string{"id": s.id, "keep_alive": "1m"})
	if err != nil {
		return nil, err
	}
	body["pit"] = pit
	if len(after) > 0 {
		body["search_after"], err = json.Marshal(after)
		if err != nil {
			return nil, fmt.Errorf("encode search checkpoint: %w", err)
		}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode PIT search: %w", err)
	}
	response, err := s.client.pitRequest(ctx, http.MethodPost, "/_search", nil, encoded)
	if err != nil {
		return nil, err
	}
	var returned struct {
		ID   string `json:"pit_id"`
		Hits struct {
			Hits []struct {
				Index string `json:"_index"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(response, &returned); err != nil || (returned.ID != "" && returned.ID != s.id) {
		return nil, errors.New("PIT search returned an unexpected snapshot identifier")
	}
	if s.expectedIndex != "" {
		for _, hit := range returned.Hits.Hits {
			if hit.Index != s.expectedIndex {
				return nil, errors.New("PIT search returned a different index")
			}
		}
	}
	return response, nil
}

func (s *Snapshot) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	body, err := json.Marshal(map[string][]string{"pit_id": {s.id}})
	if err != nil {
		return err
	}
	response, err := s.client.pitRequest(ctx, http.MethodDelete, "/_search/point_in_time", nil, body)
	if err != nil {
		return err
	}
	var result struct {
		PITs []struct {
			ID         string `json:"pit_id"`
			Successful bool   `json:"successful"`
		} `json:"pits"`
	}
	if err := json.Unmarshal(response, &result); err != nil || len(result.PITs) != 1 || result.PITs[0].ID != s.id || !result.PITs[0].Successful {
		return errors.New("PIT deletion was not confirmed")
	}
	s.closed = true
	return nil
}

func (c *Client) pitRequest(ctx context.Context, method, endpoint string, values url.Values, body []byte) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create PIT request: %w", err)
	}
	request.URL.RawQuery = values.Encode()
	if len(body) != 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Transport.Stream(request)
	if err != nil {
		return nil, fmt.Errorf("PIT network request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("PIT request failed with HTTP status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read PIT response: %w", err)
	}
	if len(data) > maxResponseBytes || !json.Valid(data) {
		return nil, errors.New("PIT response is invalid or exceeds the size limit")
	}
	return json.RawMessage(data), nil
}

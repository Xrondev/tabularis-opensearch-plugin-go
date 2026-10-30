package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"tabularis-opensearch-plugin-go/internal/query"
)

func (c *Client) OpenSingleShardPIT(ctx context.Context, operation query.Request) (query.Request, *Snapshot, error) {
	if err := operation.Validate(); err != nil {
		return query.Request{}, nil, err
	}
	index := operation.Index()
	if index == "" || strings.ContainsAny(index, "*/\\?#%[] ,") || strings.Contains(index, "..") || operation.Method() != http.MethodPost || operation.Path() != "/"+index+"/_search" {
		return query.Request{}, nil, errors.New("single-shard PIT requires one concrete index")
	}
	allowed := false
	for _, pattern := range strings.Split(c.config.IndexPattern, ",") {
		matched, err := path.Match(pattern, index)
		if err != nil {
			return query.Request{}, nil, errors.New("invalid index pattern")
		}
		allowed = allowed || matched
	}
	if !allowed {
		return query.Request{}, nil, errors.New("index is outside the configured index pattern")
	}
	before, err := c.singleShardIndexUUID(ctx, index)
	if err != nil {
		return query.Request{}, nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(operation.Body(), &body); err != nil || body == nil {
		return query.Request{}, nil, errors.New("invalid search body")
	}
	if body["from"] != nil || body["pit"] != nil || body["search_after"] != nil {
		return query.Request{}, nil, errors.New("single-shard PIT requires an initial search request")
	}
	var size int
	if err := json.Unmarshal(body["size"], &size); err != nil || size < 2 || size > 1001 {
		return query.Request{}, nil, errors.New("single-shard PIT requires a bounded positive page size")
	}
	var sorts []json.RawMessage
	if raw := body["sort"]; raw != nil {
		if err := json.Unmarshal(raw, &sorts); err != nil {
			return query.Request{}, nil, errors.New("single-shard PIT requires a sort array")
		}
	}
	for _, sort := range sorts {
		var field string
		if json.Unmarshal(sort, &field) == nil && field == "_id" {
			return query.Request{}, nil, errors.New("single-shard PIT manages the _id tie-breaker")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(sort, &fields) == nil && fields["_id"] != nil {
			return query.Request{}, nil, errors.New("single-shard PIT manages the _id tie-breaker")
		}
	}
	sorts = append(sorts, json.RawMessage(`{"_id":"asc"}`))
	encodedSorts, err := json.Marshal(sorts)
	if err != nil {
		return query.Request{}, nil, err
	}
	body["sort"] = encodedSorts
	body["size"] = json.RawMessage(strconv.Itoa(size - 1))
	envelope, err := json.Marshal(map[string]any{"index": index, "body": body})
	if err != nil {
		return query.Request{}, nil, err
	}
	prepared, err := query.Parse("#!dsl\n"+string(envelope), size-1, 1, c.config.IndexPattern)
	if err != nil {
		return query.Request{}, nil, err
	}
	snapshot, err := c.OpenPIT(ctx, prepared, true)
	if err != nil {
		return query.Request{}, nil, err
	}
	fail := func(cause error) (query.Request, *Snapshot, error) {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return query.Request{}, nil, errors.Join(cause, snapshot.Close(cleanup))
	}
	if snapshot.shards != 1 {
		return fail(errors.New("PIT did not confirm exactly one shard"))
	}
	snapshot.expectedIndex = index
	after, err := c.singleShardIndexUUID(ctx, index)
	if err != nil {
		return fail(err)
	}
	if before != after {
		return fail(errors.New("index changed while opening the PIT"))
	}
	if err := snapshot.probeIDSort(ctx); err != nil {
		return fail(err)
	}
	return prepared, snapshot, nil
}

func (c *Client) singleShardIndexUUID(ctx context.Context, index string) (string, error) {
	var resolved struct {
		Indices []struct {
			Name string `json:"name"`
		} `json:"indices"`
		Aliases     []json.RawMessage `json:"aliases"`
		DataStreams []json.RawMessage `json:"data_streams"`
	}
	if err := c.get(ctx, "/_resolve/index/"+url.PathEscape(index), url.Values{"expand_wildcards": {"none"}}, &resolved); err != nil {
		return "", fmt.Errorf("resolve concrete index: %w", err)
	}
	if resolved.Indices == nil || resolved.Aliases == nil || resolved.DataStreams == nil || len(resolved.Indices) != 1 || resolved.Indices[0].Name != index || len(resolved.Aliases) != 0 || len(resolved.DataStreams) != 0 {
		return "", errors.New("target is not exactly one concrete index")
	}
	var response map[string]struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if err := c.get(ctx, "/"+url.PathEscape(index)+"/_settings", url.Values{"flat_settings": {"true"}}, &response); err != nil {
		return "", fmt.Errorf("read index shard metadata: %w", err)
	}
	if len(response) != 1 {
		return "", errors.New("index settings did not identify one concrete index")
	}
	entry, ok := response[index]
	if !ok {
		return "", errors.New("index settings returned a different target")
	}
	var shards, uuid string
	if json.Unmarshal(entry.Settings["index.number_of_shards"], &shards) != nil || shards != "1" || json.Unmarshal(entry.Settings["index.uuid"], &uuid) != nil || uuid == "" {
		return "", errors.New("index does not expose one primary shard and a stable UUID")
	}
	return uuid, nil
}

func (s *Snapshot) probeIDSort(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("PIT snapshot is closed")
	}
	body, err := json.Marshal(map[string]any{
		"size": 1, "_source": false,
		"query": map[string]any{"match_all": map[string]any{}},
		"sort":  []any{map[string]string{"_id": "asc"}},
		"pit":   map[string]string{"id": s.id, "keep_alive": "1m"},
	})
	if err != nil {
		return err
	}
	response, err := s.client.pitRequest(ctx, http.MethodPost, "/_search", nil, body)
	if err != nil {
		return fmt.Errorf("verify PIT _id sort: %w", err)
	}
	var result struct {
		TimedOut *bool `json:"timed_out"`
		Shards   *struct {
			Total  *int `json:"total"`
			Failed *int `json:"failed"`
		} `json:"_shards"`
		Hits *struct {
			Hits []struct {
				Index string            `json:"_index"`
				Sort  []json.RawMessage `json:"sort"`
			} `json:"hits"`
		} `json:"hits"`
		PITID string `json:"pit_id"`
	}
	if err := json.Unmarshal(response, &result); err != nil || result.TimedOut == nil || *result.TimedOut || result.Shards == nil || result.Shards.Total == nil || *result.Shards.Total != 1 || result.Shards.Failed == nil || *result.Shards.Failed != 0 || result.Hits == nil || result.Hits.Hits == nil || (result.PITID != "" && result.PITID != s.id) {
		return errors.New("PIT _id sort verification returned an incomplete response")
	}
	for _, hit := range result.Hits.Hits {
		var id string
		if hit.Index != s.expectedIndex || len(hit.Sort) != 1 || json.Unmarshal(hit.Sort[0], &id) != nil || id == "" {
			return errors.New("PIT _id sort returned invalid sort values")
		}
	}
	return nil
}

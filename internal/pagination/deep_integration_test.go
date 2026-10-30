package pagination_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/connectioncache"
	"tabularis-opensearch-plugin-go/internal/pagination"
	"tabularis-opensearch-plugin-go/internal/query"
)

func TestRealPITBeyondResultWindow(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_DEEP_TEST_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("disposable OpenSearch deep fixture is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	config, err := connection.Parse(connection.Params{ConnectionID: "deep-test", Extra: map[string]string{
		"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous", "index_pattern": index,
	}})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := connectioncache.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Error(err)
		}
	})
	lease, err := cache.Acquire(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lease.Release(); err != nil {
			t.Error(err)
		}
	})
	manager, err := pagination.New(func(err error) { t.Error(err) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	body, err := json.Marshal(map[string]any{"index": index, "body": map[string]any{
		"query": map[string]any{"match_all": map[string]any{}}, "sort": []string{"sequence"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := query.Parse("#!dsl\n"+string(body), 1000, 1, index)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, 10005)
	for page := 1; page <= 11; page++ {
		raw, err := manager.Page(ctx, lease, "deep-test", "deep-session", operation, page, true)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		result, err := query.Normalize(operation, raw)
		if err != nil || result.Pagination == nil {
			t.Fatalf("page %d result: %v", page, err)
		}
		for _, row := range result.Rows {
			var id string
			if len(row) < 2 || json.Unmarshal(row[1], &id) != nil || id == "" || seen[id] {
				t.Fatalf("page %d contains a missing or duplicate ID", page)
			}
			seen[id] = true
		}
		if result.Pagination.HasMore != (page < 11) {
			t.Fatalf("page %d has_more = %v", page, result.Pagination.HasMore)
		}
	}
	if len(seen) != 10005 {
		t.Fatalf("expected 10005 documents, got %d", len(seen))
	}
	for id := 1; id <= 10005; id++ {
		if !seen[strconv.Itoa(id)] {
			t.Fatalf("missing document ID %d", id)
		}
	}
	if _, err := manager.Page(ctx, lease, "deep-test", "deep-session", operation, 10, true); err != nil {
		t.Fatalf("replay previous page: %v", err)
	}
	if err := manager.Release("deep-session"); err != nil {
		t.Fatal(err)
	}
}

func TestRealRPCPITBeyondResultWindow(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_DEEP_TEST_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("disposable OpenSearch deep fixture is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "go", "run", "../../cmd/opensearch-plugin")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := input.Close(); err != nil {
			t.Error(err)
		}
		if err := cmd.Wait(); err != nil {
			t.Errorf("plugin exit: %v: %s", err, stderr.String())
		}
	})
	encode := json.NewEncoder(input)
	decode := json.NewDecoder(output)
	envelope, err := json.Marshal(map[string]any{"index": index, "body": map[string]any{
		"query": map[string]any{"match_all": map[string]any{}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, 10005)
	for page := 1; page <= 11; page++ {
		request := map[string]any{
			"jsonrpc": "2.0", "id": page, "method": "execute_query",
			"params": map[string]any{
				"params": map[string]any{"connection_id": "rpc-deep-test", "extra": map[string]string{
					"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous",
					"index_pattern": index, "pagination_mode": "pit",
				}},
				"query": "#!dsl\n" + string(envelope), "page": page, "limit": 1000, "session_id": "rpc-deep-session",
			},
		}
		if err := encode.Encode(request); err != nil {
			t.Fatal(err)
		}
		var response struct {
			Result query.Result    `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := decode.Decode(&response); err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if response.Error != nil || response.Result.Pagination == nil || response.Result.Pagination.Page != page || response.Result.Pagination.HasMore != (page < 11) {
			t.Fatalf("invalid page %d: %s", page, response.Error)
		}
		for _, row := range response.Result.Rows {
			var id string
			if len(row) < 2 || json.Unmarshal(row[1], &id) != nil || id == "" || seen[id] {
				t.Fatalf("page %d contains a missing or duplicate ID", page)
			}
			seen[id] = true
		}
	}
	if len(seen) != 10005 {
		t.Fatalf("expected 10005 documents, got %d", len(seen))
	}
	for id := 1; id <= 10005; id++ {
		if !seen[strconv.Itoa(id)] {
			t.Fatalf("missing document ID %d", id)
		}
	}
	if err := encode.Encode(map[string]any{"jsonrpc": "2.0", "id": 12, "method": "release_session", "params": map[string]string{"session_id": "rpc-deep-session"}}); err != nil {
		t.Fatal(err)
	}
	var release struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := decode.Decode(&release); err != nil || release.Error != nil || string(release.Result) != "null" {
		t.Fatalf("release session: %s, %v", release.Error, err)
	}
}

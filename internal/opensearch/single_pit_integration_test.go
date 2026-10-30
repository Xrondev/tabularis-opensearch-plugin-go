package opensearch_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/opensearch"
	"tabularis-opensearch-plugin-go/internal/query"
)

func TestRealSingleShardProof(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_TEST_FIXTURE_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("real OpenSearch endpoint and index are required")
	}
	config, err := connection.Parse(connection.Params{Extra: map[string]string{
		"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous",
	}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := opensearch.New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	envelope, err := json.Marshal(map[string]any{
		"index": index,
		"body": map[string]any{
			"query":   map[string]any{"match_all": map[string]any{}},
			"_source": false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := query.Parse("#!dsl\n"+string(envelope), 1, 1, config.IndexPattern)
	if err != nil {
		t.Fatal(err)
	}
	prepared, snapshot, err := client.OpenSingleShardPIT(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := snapshot.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	result, err := snapshot.Search(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := query.Normalize(prepared, result); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Hits struct {
			Hits []struct {
				Sort []json.RawMessage `json:"sort"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(result, &response); err != nil || len(response.Hits.Hits) == 0 || len(response.Hits.Hits[0].Sort) == 0 {
		t.Fatal("fixture did not return a sorted document")
	}
	if _, err := snapshot.Search(context.Background(), response.Hits.Hits[0].Sort); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRealMultiShardRejected(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_TEST_MULTISHARD_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("real OpenSearch endpoint and multi-shard index are required")
	}
	config, err := connection.Parse(connection.Params{Extra: map[string]string{
		"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous",
	}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := opensearch.New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	envelope, err := json.Marshal(map[string]any{"index": index, "body": map[string]any{"query": map[string]any{"match_all": map[string]any{}}}})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := query.Parse("#!dsl\n"+string(envelope), 1, 1, config.IndexPattern)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.OpenSingleShardPIT(context.Background(), operation); err == nil {
		t.Fatal("multi-shard index was accepted")
	}
}

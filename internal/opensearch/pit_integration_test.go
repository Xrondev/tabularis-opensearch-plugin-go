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

func TestRealPITLifecycle(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	fixture := os.Getenv("OPENSEARCH_TEST_FIXTURE_INDEX")
	if endpoint == "" || fixture == "" {
		t.Skip("real OpenSearch endpoint and fixture index are required")
	}
	config, err := connection.Parse(connection.Params{Extra: map[string]string{
		"endpoint":   endpoint,
		"deployment": "self_hosted",
		"auth_mode":  "anonymous",
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
	queryEnvelope, err := json.Marshal(map[string]any{
		"index": fixture,
		"body": map[string]any{
			"query": map[string]any{"match_all": map[string]any{}},
			"sort":  []string{"count"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := query.Parse("#!dsl\n"+string(queryEnvelope), 1, 1, config.IndexPattern)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.OpenPIT(context.Background(), operation, false); err == nil {
		t.Fatal("unverified sort must not open a PIT")
	}
	snapshot, err := client.OpenPIT(context.Background(), operation, true)
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
	var search struct {
		Hits struct {
			Hits []struct {
				Sort []json.RawMessage `json:"sort"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(result, &search); err != nil {
		t.Fatal(err)
	}
	if len(search.Hits.Hits) == 0 || len(search.Hits.Hits[0].Sort) == 0 {
		t.Fatal("expected a fixture document with sort values")
	}
	if _, err := snapshot.Search(context.Background(), search.Hits.Hits[0].Sort); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Search(context.Background(), nil); err == nil {
		t.Fatal("closed PIT must reject searches")
	}
}

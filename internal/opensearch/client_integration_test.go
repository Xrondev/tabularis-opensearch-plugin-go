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

func TestRealOpenSearchMetadata(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT is not set")
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
	if err := client.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	tables, err := client.Tables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if table.Name == "" {
			t.Fatal("server returned a table without a name")
		}
		columns, err := client.Columns(context.Background(), table.Name)
		if err != nil {
			t.Fatal(err)
		}
		for _, column := range columns {
			if column.Name == "" || column.DataType == "" {
				t.Fatal("server returned a column without a name or type")
			}
		}
	}
	fixture := os.Getenv("OPENSEARCH_TEST_FIXTURE_INDEX")
	if fixture != "" {
		columns, err := client.Columns(context.Background(), fixture)
		if err != nil {
			t.Fatal(err)
		}
		actual := make(map[string]string)
		for _, column := range columns {
			actual[column.Name] = column.DataType
		}
		if actual["count"] != "long" || actual["message"] != "text" || actual["message.keyword"] != "keyword" {
			t.Fatalf("unexpected fixture mappings: %v", actual)
		}
	} else if len(tables) > 0 {
		fixture = tables[0].Name
	}
	if fixture == "" {
		t.Skip("server has no indices for search verification")
	}
	envelope, err := json.Marshal(map[string]any{
		"index": fixture,
		"body":  map[string]any{"query": map[string]any{"match_all": map[string]any{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := query.Parse("#!dsl\n"+string(envelope), 10, 1, config.IndexPattern)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Do(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := query.Normalize(operation, result)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("OPENSEARCH_TEST_FIXTURE_INDEX") != "" && len(normalized.Rows) == 0 {
		t.Fatal("expected the fixture document in search results")
	}
}

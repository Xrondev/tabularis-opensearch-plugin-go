package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/opensearch"
	"tabularis-opensearch-plugin-go/internal/query"
)

func TestRealConcreteIndexUUID(t *testing.T) {
	endpoint, index := os.Getenv("OPENSEARCH_TEST_ENDPOINT"), os.Getenv("OPENSEARCH_TEST_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT and OPENSEARCH_TEST_INDEX are required")
	}
	config, err := connection.Parse(connection.Params{Extra: map[string]string{"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous"}})
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
	first, err := client.ConcreteIndexUUID(context.Background(), index)
	if err != nil || first == "" {
		t.Fatalf("expected concrete index UUID, got %q: %v", first, err)
	}
	second, err := client.ConcreteIndexUUID(context.Background(), index)
	if err != nil || second != first {
		t.Fatalf("UUID changed: %q, %q: %v", first, second, err)
	}
	if alias := os.Getenv("OPENSEARCH_TEST_ALIAS"); alias != "" {
		if _, err := client.ConcreteIndexUUID(context.Background(), alias); err == nil {
			t.Fatal("alias accepted as concrete index")
		}
	}
	if _, err := client.ConcreteIndexUUID(context.Background(), index+"*"); err == nil {
		t.Fatal("wildcard accepted as concrete index")
	}
}

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

func TestRealOpenSearchErrorCategories(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_TEST_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT and OPENSEARCH_TEST_INDEX are required")
	}
	config, err := connection.Parse(connection.Params{Extra: map[string]string{
		"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous", "index_pattern": index,
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
	operation, err := query.Parse("#!rest\nGET /"+index+"/_doc/no-such-document?routing=private-value", 10, 1, index)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(context.Background(), operation)
	if err == nil || !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("unexpected not-found error: %v", err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err = client.Do(ctx, operation)
	if err == nil || !strings.Contains(err.Error(), "timed out") || !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("unexpected deadline error: %v", err)
	}
}

func TestRealOpenSearchAuthenticationAndTLSFailures(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_AUTH_TEST_ENDPOINT")
	ca := os.Getenv("OPENSEARCH_AUTH_TEST_CA")
	if endpoint == "" || ca == "" {
		t.Skip("OPENSEARCH_AUTH_TEST_ENDPOINT and OPENSEARCH_AUTH_TEST_CA are required")
	}
	base := map[string]string{"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "basic"}
	config, err := connection.Parse(connection.Params{Username: "private-user", Password: "private-secret", SSLCA: ca, Extra: base})
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
	_, err = client.Tables(context.Background())
	if err == nil || !strings.Contains(err.Error(), "authentication failed") || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "private-user") {
		t.Fatalf("unexpected authentication error: %v", err)
	}
	config.CAPath = ""
	clientWithoutCA, err := opensearch.New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := clientWithoutCA.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = clientWithoutCA.Tables(context.Background())
	if err == nil || !strings.Contains(err.Error(), "TLS certificate verification failed") || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("unexpected TLS error: %v", err)
	}
}

func TestRealOpenSearchPermissionFailure(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_AUTH_TEST_ENDPOINT")
	ca := os.Getenv("OPENSEARCH_AUTH_TEST_CA")
	username := os.Getenv("OPENSEARCH_RESTRICTED_TEST_USER")
	password := os.Getenv("OPENSEARCH_RESTRICTED_TEST_PASSWORD")
	if endpoint == "" || ca == "" || username == "" || password == "" {
		t.Skip("restricted OpenSearch test credentials are required")
	}
	config, err := connection.Parse(connection.Params{Username: username, Password: password, SSLCA: ca, Extra: map[string]string{
		"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "basic",
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
	_, err = client.Tables(context.Background())
	if err == nil || !strings.Contains(err.Error(), "permission denied") || !strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), password) || strings.Contains(err.Error(), username) {
		t.Fatalf("unexpected permission error: %v", err)
	}
}

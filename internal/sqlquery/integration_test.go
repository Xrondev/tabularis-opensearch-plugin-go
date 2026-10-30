package sqlquery_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"

	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/opensearch"
	"tabularis-opensearch-plugin-go/internal/sqlquery"
)

func TestRealOpenSearchSQL(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT is required")
	}
	validated, err := sqlquery.Validate(`SELECT message FROM "tabularis_fixture" WHERE count >= 2`, "tabularis_*", 20)
	if err != nil {
		t.Fatal(err)
	}
	config, err := connection.Parse(connection.Params{Extra: map[string]string{
		"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous", "index_pattern": "tabularis_*",
	}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := opensearch.New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	data, err := client.SQL(context.Background(), validated)
	if err != nil {
		t.Fatal(err)
	}
	result, err := sqlquery.Normalize(validated, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range result.Rows {
		if len(row) != 0 && string(row[0]) == `"hello"` {
			found = true
		}
	}
	if !found {
		t.Fatalf("unexpected SQL result: %s", data)
	}
	limited, err := sqlquery.Validate(`SELECT message FROM "tabularis_fixture" WHERE count >= 2 ORDER BY message`, "tabularis_*", 1)
	if err != nil {
		t.Fatal(err)
	}
	data, err = client.SQL(context.Background(), limited)
	if err != nil {
		t.Fatal(err)
	}
	limitedResult, err := sqlquery.Normalize(limited, data)
	if err != nil || len(limitedResult.Rows) != 1 || limitedResult.Truncated || !limitedResult.Pagination.HasMore {
		t.Fatalf("invalid limited SQL result: %+v, %v", limitedResult, err)
	}
	otherConfig := config
	otherConfig.IndexPattern = "other*"
	other, err := opensearch.New(otherConfig, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.SQL(context.Background(), limited); err == nil {
		t.Fatal("accepted SQL query outside connection index scope")
	}
}

func TestSQLRenderingPreservesResults(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT is required")
	}
	queries := []string{
		`SELECT message FROM tabularis_fixture WHERE count >= 2 ORDER BY message`,
		`SELECT message FROM tabularis_fixture WHERE message = 'hello'`,
		`SELECT COUNT(*) FROM tabularis_fixture`,
		`SELECT message, COUNT(*) FROM tabularis_fixture GROUP BY message ORDER BY message`,
		`SELECT message FROM tabularis_fixture WHERE count IN (1, 2, 3) ORDER BY message`,
	}
	for _, text := range queries {
		validated, err := sqlquery.Validate(text, "tabularis_*", 100)
		if err != nil {
			t.Errorf("validate %s: %v", text, err)
			continue
		}
		var rows [][][]json.RawMessage
		for _, sql := range []string{text, validated.SQL()} {
			body, err := json.Marshal(map[string]string{"query": sql})
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.Post(endpoint+"/_plugins/_sql", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			var data struct {
				Rows [][]json.RawMessage `json:"datarows"`
			}
			err = json.NewDecoder(response.Body).Decode(&data)
			response.Body.Close()
			if err != nil || response.StatusCode != http.StatusOK {
				t.Fatalf("SQL response status %d: %v; SQL: %s", response.StatusCode, err, sql)
			}
			rows = append(rows, data.Rows)
		}
		if !reflect.DeepEqual(rows[0], rows[1]) {
			t.Errorf("SQL rendering changed results for %s", text)
		}
	}
}

func TestRealSQLNumberedPages(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT is required")
	}
	config, err := connection.Parse(connection.Params{Extra: map[string]string{
		"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous", "index_pattern": "tabularis_*",
	}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := opensearch.New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, test := range []struct {
		page    int
		message string
		more    bool
	}{
		{1, `"hello"`, true},
		{2, `"three"`, false},
		{3, "", false},
	} {
		request, err := sqlquery.ValidatePage(`SELECT message FROM "tabularis_fixture" ORDER BY message LIMIT 2`, "tabularis_*", 1, test.page)
		if err != nil {
			t.Fatalf("page %d: %v", test.page, err)
		}
		data, err := client.SQL(context.Background(), request)
		if err != nil {
			t.Fatalf("page %d: %v", test.page, err)
		}
		result, err := sqlquery.Normalize(request, data)
		if err != nil {
			t.Fatalf("page %d: %v", test.page, err)
		}
		if result.Pagination.Page != test.page || result.Pagination.HasMore != test.more || result.Truncated {
			t.Fatalf("page %d: invalid metadata: %+v", test.page, result.Pagination)
		}
		if test.message == "" && len(result.Rows) != 0 {
			t.Fatalf("page %d: expected empty result", test.page)
		}
		if test.message != "" && (len(result.Rows) != 1 || string(result.Rows[0][0]) != test.message) {
			t.Fatalf("page %d: invalid rows: %s", test.page, data)
		}
	}
}

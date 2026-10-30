package browse_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"tabularis-opensearch-plugin-go/internal/browse"
	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/opensearch"
)

func TestRealBrowseSearchAndCount(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	fixture := os.Getenv("OPENSEARCH_TEST_FIXTURE_INDEX")
	if endpoint == "" || fixture == "" {
		t.Skip("real OpenSearch endpoint and fixture index are required")
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
	columns, err := client.Columns(context.Background(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		query  string
		counts []int
		count  string
	}{
		{query: fmt.Sprintf(`SELECT * FROM "%s" WHERE count >= 4 ORDER BY count DESC`, fixture), counts: []int{5, 4}},
		{query: fmt.Sprintf(`SELECT * FROM "%s" WHERE "_source.count" >= 4 ORDER BY "_source.count" DESC`, fixture), counts: []int{5, 4}},
		{query: fmt.Sprintf(`SELECT * FROM "%s" WHERE count = 3`, fixture), counts: []int{3}},
		{query: fmt.Sprintf(`SELECT * FROM "%s" WHERE count IN (3, 5) ORDER BY count ASC`, fixture), counts: []int{3, 5}},
		{query: fmt.Sprintf(`SELECT * FROM "%s" WHERE count != 4 ORDER BY count ASC`, fixture), counts: []int{3, 5}},
		{query: fmt.Sprintf(`SELECT * FROM "%s" ORDER BY count ASC LIMIT 1`, fixture), counts: []int{3}},
		{query: fmt.Sprintf(`SELECT COUNT(*) FROM "%s" WHERE count >= 4`, fixture), count: "2"},
		{query: fmt.Sprintf(`SELECT COUNT(*) AS count FROM "%s"`, fixture), count: "3"},
	}
	for _, test := range tests {
		plan, err := browse.Translate(test.query, config.IndexPattern, 100, 1, columns, map[string]bool{"count": true})
		if err != nil {
			t.Fatalf("%s: %v", test.query, err)
		}
		raw, err := client.Do(context.Background(), plan.Request)
		if err != nil {
			t.Fatalf("%s: %v", test.query, err)
		}
		result, err := browse.Normalize(plan, raw)
		if err != nil {
			t.Fatalf("%s: %v", test.query, err)
		}
		if test.count != "" {
			if len(result.Rows) != 1 || string(result.Rows[0][0]) != test.count {
				t.Fatalf("%s: unexpected count result", test.query)
			}
			continue
		}
		if len(result.Rows) != len(test.counts) {
			t.Fatalf("%s: unexpected number of rows", test.query)
		}
		for i, row := range result.Rows {
			var source struct {
				Count int `json:"count"`
			}
			var text string
			if len(row) < 4 || json.Unmarshal(row[3], &text) != nil || json.Unmarshal([]byte(text), &source) != nil || source.Count != test.counts[i] {
				t.Fatalf("%s: unexpected document", test.query)
			}
		}
	}
	if _, err := browse.Translate(fmt.Sprintf(`SELECT * FROM "%s" WHERE count = 3`, fixture), config.IndexPattern, 100, 1, columns, nil); err == nil {
		t.Fatal("unverified field semantics were accepted")
	}
	if _, err := browse.Translate(fmt.Sprintf(`SELECT * FROM "%s" WHERE "_source.count" = 3`, fixture), config.IndexPattern, 100, 1, columns, nil); err == nil {
		t.Fatal("unverified generated field semantics were accepted")
	}
	if _, err := browse.Translate(fmt.Sprintf(`SELECT * FROM "%s" WHERE "_source.message.keyword" = 'hello'`, fixture), config.IndexPattern, 100, 1, columns, map[string]bool{"message.keyword": true}); err == nil {
		t.Fatal("ambiguous nested or multi-field representation was accepted")
	}
	plan, err := browse.Translate(fmt.Sprintf(`SELECT * FROM "%s"`, fixture), config.IndexPattern, 100, 1, columns, nil)
	if err != nil {
		t.Fatalf("unfiltered browse required field semantics: %v", err)
	}
	raw, err := client.Do(context.Background(), plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := browse.Normalize(plan, raw)
	if err != nil || len(result.Rows) != 3 || len(result.Columns) < 4 || result.Columns[0] != "_index" || result.Columns[1] != "_id" || result.Columns[3] != "_source" {
		t.Fatal("unfiltered browse did not retain the fixture documents and metadata")
	}
}

func TestRealBrowseBoundedPages(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	fixture := os.Getenv("OPENSEARCH_TEST_FIXTURE_INDEX")
	if endpoint == "" || fixture == "" {
		t.Skip("real OpenSearch endpoint and fixture index are required")
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
	columns, err := client.Columns(context.Background(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		limit  string
		page   int
		rows   int
		more   bool
		counts []int
	}{
		{"", 1, 2, true, []int{3, 4}},
		{"", 2, 1, false, []int{5}},
		{"", 3, 0, false, nil},
		{" LIMIT 0", 1, 0, false, nil},
		{" LIMIT 1", 1, 1, false, []int{3}},
		{" LIMIT 1", 2, 0, false, nil},
		{" LIMIT 2", 1, 2, false, []int{3, 4}},
		{" LIMIT 2", 2, 0, false, nil},
		{" LIMIT 3", 1, 2, true, []int{3, 4}},
		{" LIMIT 3", 2, 1, false, []int{5}},
	}
	for _, test := range cases {
		sql := fmt.Sprintf(`SELECT * FROM "%s" ORDER BY count ASC%s`, fixture, test.limit)
		plan, err := browse.Translate(sql, config.IndexPattern, 2, test.page, columns, map[string]bool{"count": true})
		if err != nil {
			t.Fatalf("%s page %d: %v", sql, test.page, err)
		}
		raw, err := client.Do(context.Background(), plan.Request)
		if err != nil {
			t.Fatal(err)
		}
		result, err := browse.Normalize(plan, raw)
		if err != nil || len(result.Rows) != test.rows || result.Pagination == nil || result.Pagination.Page != test.page || result.Pagination.HasMore != test.more {
			t.Fatalf("%s page %d: result=%+v err=%v", sql, test.page, result, err)
		}
		if test.limit == " LIMIT 1" && test.page == 2 && result.Pagination.TotalRows != nil {
			t.Fatal("exhausted page reported an invented total")
		}
		if test.limit == " LIMIT 0" && (result.Pagination.TotalRows == nil || *result.Pagination.TotalRows != 0) {
			t.Fatal("LIMIT 0 did not report its exact empty result")
		}
		for i, row := range result.Rows {
			var text string
			var source struct {
				Count int `json:"count"`
			}
			if len(row) < 4 || json.Unmarshal(row[3], &text) != nil || json.Unmarshal([]byte(text), &source) != nil || source.Count != test.counts[i] {
				t.Fatalf("%s page %d returned an unexpected document", sql, test.page)
			}
		}
	}
	countCases := []struct {
		limit string
		page  int
		rows  int
	}{
		{"", 1, 1}, {"", 2, 0}, {" LIMIT 0", 1, 0}, {" LIMIT 1", 1, 1}, {" LIMIT 1", 2, 0},
	}
	for _, test := range countCases {
		countSQL := fmt.Sprintf(`SELECT COUNT(*) AS count FROM "%s"%s`, fixture, test.limit)
		plan, err := browse.Translate(countSQL, config.IndexPattern, 2, test.page, columns, nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := client.Do(context.Background(), plan.Request)
		if err != nil {
			t.Fatal(err)
		}
		result, err := browse.Normalize(plan, raw)
		if err != nil || len(result.Rows) != test.rows {
			t.Fatalf("%s page %d: result=%+v err=%v", countSQL, test.page, result, err)
		}
	}
}

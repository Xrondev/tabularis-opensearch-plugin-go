package query_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/opensearch"
	"tabularis-opensearch-plugin-go/internal/query"
)

func TestRealOpenSearchReadOnlyQuery(t *testing.T) {
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
	defer client.Close()
	req, err := query.Parse("#!rest\nPOST /"+index+"/_search\n{\"query\":{\"match_all\":{}}}", 2, 1, index)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	result, err := query.Normalize(req, response)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pagination == nil || result.Pagination.PageSize != 2 {
		t.Fatalf("invalid search result: %+v", result)
	}
}

func TestRealInspectionEndpoints(t *testing.T) {
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
	defer client.Close()
	for _, item := range []struct {
		request string
		key     string
	}{
		{fmt.Sprintf("#!rest\nGET /%s/_count", index), "count"},
		{fmt.Sprintf("#!rest\nPOST /%s/_count\n{\"query\":{\"match_all\":{}}}", index), "count"},
		{fmt.Sprintf("#!rest\nGET /%s/_doc/1", index), "_id"},
		{fmt.Sprintf("#!rest\nGET /%s/_field_caps?fields=*", index), "fields"},
		{fmt.Sprintf("#!rest\nPOST /%s/_explain/1\n{\"query\":{\"match_all\":{}}}", index), "matched"},
	} {
		req, err := query.Parse(item.request, 100, 1, index)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := client.Do(context.Background(), req)
		if err != nil {
			t.Fatalf("%s: %v", item.request, err)
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(raw, &result); err != nil || result[item.key] == nil {
			t.Fatalf("%s: invalid response %s", item.request, raw)
		}
	}
}

func TestRealBoundedPagination(t *testing.T) {
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
	defer client.Close()
	text := fmt.Sprintf("#!dsl\n{\"index\":%q,\"body\":{\"sort\":[{\"count\":\"asc\"}]}}", index)
	for page, want := range []struct {
		count int
		more  bool
	}{{2, true}, {1, false}} {
		req, err := query.Parse(text, 2, page+1, index)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := client.Do(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		result, err := query.Normalize(req, raw)
		if err != nil || result.Pagination == nil || result.Pagination.Page != page+1 || len(result.Rows) != want.count || result.Pagination.HasMore != want.more {
			t.Fatalf("page %d: %+v, %v", page+1, result, err)
		}
	}
}

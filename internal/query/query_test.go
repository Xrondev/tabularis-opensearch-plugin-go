package query_test

import (
	"encoding/json"
	"strings"
	"testing"

	"tabularis-opensearch-plugin-go/internal/query"
)

func TestDSLSearch(t *testing.T) {
	req, err := query.Parse("#!dsl\n{\"index\":\"events\",\"body\":{\"query\":{\"match_all\":{}},\"size\":2}}", 100, 1, "events*")
	if err != nil {
		t.Fatal(err)
	}
	if req.Method() != "POST" || req.Path() != "/events/_search" || req.Index() != "events" {
		t.Fatal("incorrect request")
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(req.Body(), &body); err != nil || string(body["size"]) != "3" {
		t.Fatalf("incorrect search body: %s", req.Body())
	}
	response := []byte(`{"timed_out":false,"_shards":{"failed":0},"hits":{"total":{"value":3,"relation":"eq"},"hits":[{"_index":"events","_id":"1","_source":{"count":9007199254740993}},{"_index":"events","_id":"2","_source":{"name":"x"}},{"_index":"events","_id":"3","_source":{}}]},"aggregations":{"total":{"value":3}}}`)
	result, err := query.Normalize(req, response)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 || result.Pagination == nil || !result.Pagination.HasMore || *result.Pagination.TotalRows != 3 {
		t.Fatalf("incorrect page: %+v", result)
	}
	var source string
	if err := json.Unmarshal(result.Rows[0][3], &source); err != nil || source != `{"count":9007199254740993}` {
		t.Fatalf("original source was not retained: %q, %v", source, err)
	}
	if string(result.Rows[0][4]) != `"9007199254740993"` {
		t.Fatalf("large integer was not preserved: %s", result.Rows[0][4])
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), "9007199254740993") || !strings.Contains(string(encoded), `"additional_results"`) {
		t.Fatalf("lost result content: %s, %v", encoded, err)
	}
}

func TestLargeTotalRemainsExact(t *testing.T) {
	req, err := query.Parse("#!dsl\n{\"index\":\"events\",\"body\":{}}", 10, 1, "events")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"timed_out":false,"_shards":{"failed":0},"hits":{"total":{"value":9007199254740993,"relation":"eq"},"hits":[]}}`)
	result, err := query.Normalize(req, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pagination.TotalRows != nil {
		t.Fatalf("unsafe total: %d", *result.Pagination.TotalRows)
	}
	encoded, err := json.Marshal(result.AdditionalResults)
	if err != nil || !strings.Contains(string(encoded), "9007199254740993") {
		t.Fatalf("lost exact total: %s", encoded)
	}
}

func TestInspectionRequests(t *testing.T) {
	for _, text := range []string{
		"#!rest\nGET /events/_count",
		"#!rest\nPOST /events/_count\n{\"query\":{\"match_all\":{}}}",
		"#!rest\nGET /events/_doc/a%2Fb?routing=route%2F1",
		"#!rest\nGET /events/_field_caps?fields=name%2Ccount",
		"#!rest\nPOST /events/_explain/a%2Fb\n{\"query\":{\"match_all\":{}}}",
	} {
		if _, err := query.Parse(text, 100, 1, "events"); err != nil {
			t.Errorf("%s: %v", text, err)
		}
	}
	for _, text := range []string{
		"#!rest\nPOST /events/_count\n{\"query\":{\"script\":{}}}",
		"#!rest\nGET /events/_doc/a%2Fb?refresh=true",
		"#!rest\nGET /events/_field_caps?fields=name&fields=secret",
		"#!rest\nPOST /events/_explain/a\n{\"query\":{\"script_score\":{}}}",
		"#!rest\nGET /events/_doc/a%2F..%2Fb",
	} {
		if _, err := query.Parse(text, 100, 1, "events"); err == nil {
			t.Errorf("accepted unsafe request: %s", text)
		}
	}
}

func TestBoundedSearchPages(t *testing.T) {
	req, err := query.Parse("#!dsl\n{\"index\":\"events\",\"body\":{\"query\":{\"match_all\":{}}}}", 2, 2, "events")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(req.Body(), &body); err != nil || string(body["from"]) != "2" || string(body["size"]) != "3" {
		t.Fatalf("invalid page request: %s", req.Body())
	}
	response := []byte(`{"timed_out":false,"_shards":{"failed":0},"hits":{"total":{"value":3,"relation":"eq"},"hits":[{"_index":"events","_id":"3","_source":{}}]}}`)
	result, err := query.Normalize(req, response)
	if err != nil || result.Pagination == nil || result.Pagination.Page != 2 || result.Pagination.HasMore {
		t.Fatalf("invalid page response: %+v, %v", result, err)
	}
	if _, err := query.Parse("#!dsl\n{\"index\":\"events\",\"body\":{}}", 1000, 11, "events"); err == nil {
		t.Fatal("page exceeding result window accepted")
	}
}

func TestRejectIncompleteSearchResponses(t *testing.T) {
	req, err := query.Parse("#!dsl\n{\"index\":\"events\",\"body\":{}}", 10, 1, "events")
	if err != nil {
		t.Fatal(err)
	}
	for _, response := range []string{
		`{"hits":{"hits":[]}}`,
		`{"timed_out":false,"_shards":{},"hits":{"hits":[]}}`,
		`{"timed_out":false,"_shards":{"failed":0},"hits":{}}`,
		`{"timed_out":false,"_shards":{"failed":0},"hits":{"hits":null}}`,
	} {
		if _, err := query.Normalize(req, []byte(response)); err == nil {
			t.Errorf("accepted incomplete response: %s", response)
		}
	}
}

func TestRejectIncompleteCount(t *testing.T) {
	req, err := query.Parse("#!rest\nGET /events/_count", 10, 1, "events")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"count":2}`,
		`{"count":2,"_shards":{"failed":1}}`,
		`{"count":2,"_shards":{}}`,
	} {
		if _, err := query.Normalize(req, []byte(raw)); err == nil {
			t.Errorf("accepted incomplete count: %s", raw)
		}
	}
}

func TestLosslessScalar(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{"9007199254740992", "9007199254740992"},
		{"9007199254740993", `"9007199254740993"`},
		{"0.5", "0.5"},
		{"0.1", `"0.1"`},
		{"1e999", `"1e999"`},
		{"1e-999999999", `"1e-999999999"`},
		{"0", "0"},
		{`"text"`, `"text"`},
	} {
		if got := string(query.LosslessScalar(json.RawMessage(test.input))); got != test.want {
			t.Errorf("%s: got %s, want %s", test.input, got, test.want)
		}
	}
}

func TestRejectUnsafeRequests(t *testing.T) {
	for _, text := range []string{
		"#!rest\nDELETE /events/_search",
		"#!rest\nPOST /events/_delete_by_query\n{}",
		"#!rest\nPOST /_plugins/_sql\n{\"query\":\"DELETE FROM events\"}",
		"#!rest\nGET /%2e%2e/_mapping",
		"#!rest\nGET https://example.com/events/_mapping",
		"#!rest\nGET /events/_mapping?pretty=true",
		"#!dsl\n{\"index\":\"private\",\"body\":{}}",
		"#!dsl\n{\"index\":\"events\",\"body\":{\"from\":1}}",
		"#!dsl\n{\"index\":\"events\",\"body\":{\"terminate_after\":1}}",
		"#!dsl\n{\"index\":\"events\",\"body\":{\"query\":{\"script_score\":{\"script\":\"bad\"}}}}",
		"#!dsl\n{\"index\":\"events\",\"body\":{\"size\":0,\"size\":1}}",
		"#!dsl\n{\"index\":\"events\",\"body\":{\"size\":1001}}",
		"#!dsl\n{\"index\":\"events\",\"body\":{\"size\":1.2}}",
	} {
		if _, err := query.Parse(text, 100, 1, "events*"); err == nil {
			t.Errorf("accepted unsafe query: %s", text)
		}
	}
}

func TestAggregationAndIncompleteSearch(t *testing.T) {
	req, err := query.Parse("#!dsl\n{\"index\":\"events\",\"body\":{\"size\":0}}", 100, 1, "*")
	if err != nil {
		t.Fatal(err)
	}
	result, err := query.Normalize(req, []byte(`{"timed_out":false,"_shards":{"failed":0},"aggregations":{"value":1}}`))
	if err != nil || result.Pagination != nil || len(result.Rows) != 1 {
		t.Fatalf("invalid aggregation result: %+v, %v", result, err)
	}
	if _, err := query.Normalize(req, []byte(`{"timed_out":true,"_shards":{"failed":0}}`)); err == nil {
		t.Fatal("accepted incomplete aggregation")
	}
	if _, err := query.Normalize(req, []byte(`{"terminated_early":true,"_shards":{"failed":0}}`)); err == nil {
		t.Fatal("accepted early terminated aggregation")
	}
	if _, err := query.Parse("#!dsl\n{\"index\":\"events\",\"body\":{\"size\":0}}", 100, 2, "*"); err == nil {
		t.Fatal("accepted aggregation-only second page")
	}
}

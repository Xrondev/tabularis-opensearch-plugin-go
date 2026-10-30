package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestExecutableConnectsToOpenSearch(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT is not set")
	}
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "test_connection",
		"params": map[string]any{"params": map[string]any{"extra": map[string]string{
			"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous",
		}}},
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("run plugin: %v", err)
	}
	var response struct {
		Result struct {
			Success bool `json:"success"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.Success {
		t.Fatalf("connection failed: %s", output)
	}
}

func TestExecutableVerifiedNullFilters(t *testing.T) {
	endpoint, index := os.Getenv("OPENSEARCH_TEST_ENDPOINT"), os.Getenv("OPENSEARCH_NULL_TEST_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("null-filter fixture is not configured")
	}
	for _, tc := range []struct {
		predicate string
		ids       map[string]bool
	}{
		{`"_source.count" IS NULL`, map[string]bool{"2": true, "3": true}},
		{`"_source.count" IS NOT NULL`, map[string]bool{"1": true}},
	} {
		t.Run(tc.predicate, func(t *testing.T) {
			request := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "execute_query", "params": map[string]any{"params": map[string]any{"extra": map[string]string{"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous", "index_pattern": index, "verified_exact_fields": "count"}}, "query": "SELECT * FROM \"" + index + "\" WHERE " + tc.predicate, "limit": 10, "page": 1}}
			input, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "run", ".")
			cmd.Stdin = bytes.NewReader(append(input, '\n'))
			output, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				Result struct {
					Rows [][]json.RawMessage `json:"rows"`
				} `json:"result"`
				Error json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(output, &response); err != nil || len(response.Error) != 0 {
				t.Fatalf("query failed: %s %v", output, err)
			}
			if len(response.Result.Rows) != len(tc.ids) {
				t.Fatalf("unexpected rows: %s", output)
			}
			for _, row := range response.Result.Rows {
				var id string
				if len(row) < 2 || json.Unmarshal(row[1], &id) != nil || !tc.ids[id] {
					t.Fatalf("unexpected row: %s", output)
				}
			}
		})
	}
}

func TestExecutablePITPages(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_TEST_FIXTURE_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT and OPENSEARCH_TEST_FIXTURE_INDEX are required")
	}
	cmd := exec.Command("go", "run", ".")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stdin.Close()
		cmd.Wait()
	}()
	encoder := json.NewEncoder(stdin)
	decoder := json.NewDecoder(bufio.NewReader(stdout))
	ids := make(map[string]bool)
	for page := 1; page <= 2; page++ {
		request := map[string]any{
			"jsonrpc": "2.0", "id": page, "method": "execute_query",
			"params": map[string]any{
				"params": map[string]any{"connection_id": "pit-test", "extra": map[string]string{
					"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous",
					"index_pattern": index, "pagination_mode": "pit",
				}},
				"session_id": "pit-session", "query": "SELECT * FROM \"" + index + "\" LIMIT 2", "limit": 1, "page": page,
			},
		}
		if err := encoder.Encode(request); err != nil {
			t.Fatal(err)
		}
		var response struct {
			Result struct {
				Rows       [][]json.RawMessage `json:"rows"`
				Pagination struct {
					Page    int  `json:"page"`
					HasMore bool `json:"has_more"`
				} `json:"pagination"`
			} `json:"result"`
			Error json.RawMessage `json:"error"`
		}
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if len(response.Error) != 0 || len(response.Result.Rows) != 1 || response.Result.Pagination.Page != page {
			t.Fatalf("PIT page %d failed: %+v", page, response)
		}
		if response.Result.Pagination.HasMore != (page == 1) {
			t.Fatalf("PIT page %d reported incorrect continuation", page)
		}
		id := string(response.Result.Rows[0][1])
		if ids[id] {
			t.Fatalf("duplicate PIT document: %s", id)
		}
		ids[id] = true
	}
	params := map[string]any{
		"params": map[string]any{"connection_id": "pit-test", "extra": map[string]string{
			"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous",
			"index_pattern": index, "pagination_mode": "pit",
		}},
		"session_id": "pit-session", "limit": 1, "page": 2,
		"query": "SELECT * FROM \"" + index + "\" LIMIT 3",
	}
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 10, "method": "execute_query", "params": params}); err != nil {
		t.Fatal(err)
	}
	var changed struct {
		Error json.RawMessage `json:"error"`
	}
	if err := decoder.Decode(&changed); err != nil || len(changed.Error) == 0 {
		t.Fatalf("changed PIT query was accepted: %s %v", changed.Error, err)
	}
	params["query"] = "#!sql\nSELECT COUNT(*) FROM \"" + index + "\""
	params["page"] = 1
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 11, "method": "execute_query", "params": params}); err != nil {
		t.Fatal(err)
	}
	var count struct {
		Error json.RawMessage `json:"error"`
	}
	if err := decoder.Decode(&count); err != nil || len(count.Error) != 0 {
		t.Fatalf("SQL count failed: %s %v", count.Error, err)
	}
	params["query"] = "SELECT * FROM \"" + index + "\" LIMIT 2"
	params["page"] = 2
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 12, "method": "execute_query", "params": params}); err != nil {
		t.Fatal(err)
	}
	var stale struct {
		Error json.RawMessage `json:"error"`
	}
	if err := decoder.Decode(&stale); err != nil || len(stale.Error) == 0 {
		t.Fatalf("stale PIT session was accepted: %s %v", stale.Error, err)
	}
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "release_session", "params": map[string]string{"session_id": "pit-session"}}); err != nil {
		t.Fatal(err)
	}
	var release struct {
		Error json.RawMessage `json:"error"`
	}
	if err := decoder.Decode(&release); err != nil || len(release.Error) != 0 {
		t.Fatalf("release failed: %s %v", release.Error, err)
	}
}

func TestPITInspectionRejectsSecondPage(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_TEST_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT and OPENSEARCH_TEST_INDEX are required")
	}
	request := map[string]any{
		"jsonrpc": "2.0", "id": 7, "method": "execute_query",
		"params": map[string]any{
			"params": map[string]any{"extra": map[string]string{
				"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous",
				"index_pattern": index, "pagination_mode": "pit",
			}},
			"query": "#!rest\nGET /" + index + "/_mapping", "limit": 10, "page": 2,
		},
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error rpcError `json:"error"`
	}
	if err := json.Unmarshal(output, &response); err != nil || response.Error.Code != -32602 {
		t.Fatalf("expected invalid page: %s", output)
	}
}

func TestExecutableExecutesDSL(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_TEST_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT and OPENSEARCH_TEST_INDEX are required")
	}
	envelope, err := json.Marshal(map[string]any{
		"index": index,
		"body":  map[string]any{"query": map[string]any{"match_all": map[string]any{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "execute_query",
		"params": map[string]any{
			"params": map[string]any{"extra": map[string]string{
				"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous", "index_pattern": index,
			}},
			"query": "#!dsl\n" + string(envelope), "limit": 10, "page": 1,
		},
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("run plugin: %v", err)
	}
	var response struct {
		Result struct {
			Columns []string `json:"columns"`
			Rows    [][]any  `json:"rows"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Error) > 0 || len(response.Result.Columns) == 0 || len(response.Result.Rows) == 0 {
		t.Fatalf("query failed: %s", output)
	}
}

func TestExecutableExecutesSQL(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_TEST_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT and OPENSEARCH_TEST_INDEX are required")
	}
	request := map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "execute_query",
		"params": map[string]any{
			"params": map[string]any{"extra": map[string]string{
				"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous", "index_pattern": index,
			}},
			"query": "#!sql\nSELECT COUNT(*) FROM \"" + index + "\"", "limit": 10, "page": 1,
		},
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("run plugin: %v", err)
	}
	var response struct {
		Result struct {
			Columns []string            `json:"columns"`
			Rows    [][]json.RawMessage `json:"rows"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Error) != 0 || len(response.Result.Columns) != 1 || len(response.Result.Rows) != 1 {
		t.Fatalf("SQL query failed: %s", output)
	}
}

func TestExecutableOpensIndex(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_TEST_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT and OPENSEARCH_TEST_INDEX are required")
	}
	request := map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "execute_query",
		"params": map[string]any{
			"params": map[string]any{"extra": map[string]string{
				"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous", "index_pattern": index,
			}},
			"query": "SELECT * FROM \"" + index + "\"", "limit": 10, "page": 1,
		},
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("run plugin: %v", err)
	}
	var response struct {
		Result struct {
			Columns []string            `json:"columns"`
			Rows    [][]json.RawMessage `json:"rows"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Error) != 0 || len(response.Result.Rows) == 0 || len(response.Result.Columns) < 4 || response.Result.Columns[0] != "_index" || response.Result.Columns[1] != "_id" || response.Result.Columns[3] != "_source" {
		t.Fatalf("index browsing failed: %s", output)
	}
}

func TestExecutableFiltersVerifiedGridField(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_TEST_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT and OPENSEARCH_TEST_INDEX are required")
	}
	request := map[string]any{
		"jsonrpc": "2.0", "id": 15, "method": "execute_query",
		"params": map[string]any{
			"params": map[string]any{"extra": map[string]string{
				"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous",
				"index_pattern": index, "verified_exact_fields": "count",
			}},
			"query": "SELECT * FROM \"" + index + "\" WHERE \"_source.count\" >= 4 ORDER BY \"_source.count\" DESC", "limit": 10, "page": 1,
		},
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result struct {
			Rows [][]json.RawMessage `json:"rows"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Error) != 0 || len(response.Result.Rows) != 2 || string(response.Result.Rows[0][1]) != `"3"` || string(response.Result.Rows[1][1]) != `"2"` {
		t.Fatalf("filtered grid query failed: %s", output)
	}
	params := request["params"].(map[string]any)
	connection := params["params"].(map[string]any)
	extra := connection["extra"].(map[string]string)
	delete(extra, "verified_exact_fields")
	data, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("go", "run", ".")
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	output, err = cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var rejected struct {
		Error rpcError `json:"error"`
	}
	if err := json.Unmarshal(output, &rejected); err != nil || rejected.Error.Code != -32602 {
		t.Fatalf("unverified field was not rejected: %s", output)
	}
	extra["verified_exact_fields"] = "message"
	params["query"] = "SELECT * FROM \"" + index + "\" WHERE \"_source.message\" = 'hello'"
	data, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("go", "run", ".")
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	output, err = cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output, &rejected); err != nil || rejected.Error.Code != -32602 || !strings.Contains(rejected.Error.Message, "does not support") {
		t.Fatalf("unsupported text field was not rejected: %s", output)
	}
}

func TestExecutableRejectsAliasForVerifiedField(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	alias := os.Getenv("OPENSEARCH_TEST_ALIAS")
	if endpoint == "" || alias == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT and OPENSEARCH_TEST_ALIAS are required")
	}
	request := map[string]any{
		"jsonrpc": "2.0", "id": 16, "method": "execute_query",
		"params": map[string]any{
			"params": map[string]any{"extra": map[string]string{
				"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous",
				"index_pattern": "*", "verified_exact_fields": "count",
			}},
			"query": "SELECT * FROM \"" + alias + "\" WHERE \"_source.count\" = 3", "limit": 10, "page": 1,
		},
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error rpcError `json:"error"`
	}
	if err := json.Unmarshal(output, &response); err != nil || !strings.Contains(response.Error.Message, "concrete index") {
		t.Fatalf("verified alias was not rejected: %s", output)
	}
}

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestStdioMethods(t *testing.T) {
	input := bytes.NewBufferString("{\"jsonrpc\":\"2.0\",\"id\":1234567890123456789,\"method\":\"get_databases\"}\n{\"jsonrpc\":\"2.0\",\"id\":\"x\",\"method\":\"unknown\"}\n")
	var output bytes.Buffer
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var first, second map[string]json.RawMessage
	if err := decoder.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&second); err != nil {
		t.Fatal(err)
	}
	if string(first["id"]) == `"x"` {
		first, second = second, first
	}
	if string(first["id"]) != "1234567890123456789" || string(first["result"]) != `["default"]` {
		t.Fatalf("unexpected response: %s", first)
	}
	var rpcErr struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(second["error"], &rpcErr); err != nil || rpcErr.Code != -32601 {
		t.Fatalf("unexpected error: %s", second["error"])
	}
}

func TestInvalidVerifiedFieldIsRejectedBeforeConnection(t *testing.T) {
	input := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"test_connection","params":{"params":{"extra":{"endpoint":"http://127.0.0.1:1","deployment":"self_hosted","auth_mode":"anonymous","verified_exact_fields":"nested.field"}}}}` + "\n")
	var output bytes.Buffer
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error rpcError `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != -32602 {
		t.Fatalf("expected invalid configuration, got %s", output.String())
	}
}

func TestUnsupportedBrowsePredicateDoesNotFetchMapping(t *testing.T) {
	input := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"execute_query","params":{"params":{"extra":{"endpoint":"http://127.0.0.1:1","deployment":"self_hosted","auth_mode":"anonymous","verified_exact_fields":"count"}},"query":"SELECT * FROM \"events\" WHERE 3 = \"_source.count\"","limit":10,"page":1}}` + "\n")
	var output bytes.Buffer
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error rpcError `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != -32602 {
		t.Fatalf("expected unsupported predicate error, got %s", output.String())
	}
}

func TestInvalidBrowseExpressionsDoNotContactServer(t *testing.T) {
	queries := []string{
		`SELECT * FROM "events" WHERE "_source.count" IS TRUE`,
		`SELECT * FROM "events" WHERE "_source.count" = 3 AND "_source.count" IS TRUE`,
		`SELECT * FROM "events" WHERE "_source.count" = 3 ORDER BY "_source.count" + 1`,
	}
	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			connected := make(chan struct{}, 1)
			release := make(chan struct{})
			defer close(release)
			go func() {
				conn, err := listener.Accept()
				if err == nil {
					connected <- struct{}{}
					<-release
					conn.Close()
				}
			}()
			request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "execute_query", "params": map[string]any{"params": map[string]any{"extra": map[string]string{"endpoint": "http://" + listener.Addr().String(), "deployment": "self_hosted", "auth_mode": "anonymous", "verified_exact_fields": "count", "request_timeout_ms": "1000"}}, "query": query, "limit": 10, "page": 1}})
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := serve(bytes.NewReader(append(request, '\n')), &output); err != nil {
				t.Fatal(err)
			}
			var response struct {
				Error rpcError `json:"error"`
			}
			if err := json.Unmarshal(output.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error.Code != -32602 {
				t.Fatalf("expected invalid query: %s", output.String())
			}
			select {
			case <-connected:
				t.Fatal("invalid query contacted server")
			default:
			}
		})
	}
}

func TestVerifiedFieldsRejectWildcardBeforeConnection(t *testing.T) {
	input := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"execute_query","params":{"params":{"extra":{"endpoint":"http://127.0.0.1:1","deployment":"self_hosted","auth_mode":"anonymous","index_pattern":"events*","verified_exact_fields":"count"}},"query":"SELECT * FROM \"events*\" WHERE \"_source.count\" = 3","limit":10,"page":1}}` + "\n")
	var output bytes.Buffer
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error rpcError `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != -32602 || !strings.Contains(response.Error.Message, "concrete index") {
		t.Fatalf("expected concrete-index validation, got %s", output.String())
	}
}

func TestUnverifiedBrowseFieldDoesNotFetchMapping(t *testing.T) {
	input := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"execute_query","params":{"params":{"extra":{"endpoint":"http://127.0.0.1:1","deployment":"self_hosted","auth_mode":"anonymous","verified_exact_fields":"count"}},"query":"SELECT * FROM \"events\" WHERE \"_source.other\" = 3","limit":10,"page":1}}` + "\n")
	var output bytes.Buffer
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error rpcError `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != -32602 || !strings.Contains(response.Error.Message, "verified") {
		t.Fatalf("expected field verification error, got %s", output.String())
	}
}

func TestExecutableCancelsInFlightRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	started := make(chan struct{})
	canceled := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\r\n" {
				break
			}
		}
		close(started)
		_, _ = io.Copy(io.Discard, reader)
		close(canceled)
	}()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=TestPluginSubprocess")
	cmd.Env = append(os.Environ(), "PLUGIN_SUBPROCESS=1")
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
	defer cmd.Process.Kill()
	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 17, "method": "test_connection",
		"params": map[string]any{"params": map[string]any{"extra": map[string]string{
			"endpoint": "http://" + listener.Addr().String(), "deployment": "self_hosted", "auth_mode": "anonymous",
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write(append(request, '\n')); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(20 * time.Second):
		t.Fatal("request did not start")
	}
	select {
	case <-canceled:
		t.Fatal("request ended before cancellation")
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := io.WriteString(stdin, "{\"jsonrpc\":\"2.0\",\"method\":\"cancel\",\"params\":{\"id\":17}}\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP request was not canceled")
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "\n") != 1 {
		t.Fatalf("expected one request response and no cancel response: %s", data)
	}
}

func TestPluginSubprocess(t *testing.T) {
	if os.Getenv("PLUGIN_SUBPROCESS") != "1" {
		return
	}
	main()
	os.Exit(0)
}

func TestInvalidRequestPreservesID(t *testing.T) {
	input := bytes.NewBufferString("{\"jsonrpc\":\"1.0\",\"id\":\"request-1\",\"method\":\"get_databases\"}\n")
	var output bytes.Buffer
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		ID    string   `json:"id"`
		Error rpcError `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != "request-1" || result.Error.Code != -32600 {
		t.Fatalf("unexpected response: %s", output.String())
	}
}

func TestEmptyLineDoesNotEndProtocol(t *testing.T) {
	input := bytes.NewBufferString("\n{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"get_databases\"}\n")
	var output bytes.Buffer
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var first, second map[string]json.RawMessage
	if err := decoder.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&second); err != nil {
		t.Fatal(err)
	}
	if len(first["error"]) == 0 || string(second["result"]) != `["default"]` {
		t.Fatalf("unexpected responses: %s", output.String())
	}
}

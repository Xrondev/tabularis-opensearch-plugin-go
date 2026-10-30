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

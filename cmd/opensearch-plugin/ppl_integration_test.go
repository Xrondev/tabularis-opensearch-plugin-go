package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecutablePPL(t *testing.T) {
	endpoint, index, bundle := os.Getenv("PPL_TEST_ENDPOINT"), os.Getenv("PPL_TEST_INDEX"), os.Getenv("PPL_TEST_BUNDLE")
	if bundle == "" {
		t.Skip("PPL_TEST_BUNDLE is required")
	}
	if index == "" {
		index = "tabularis_ppl_fixture"
	}
	if err := os.MkdirAll("../../bin", 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("../../bin", "ppl-integration-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	dir, err = filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "opensearch-plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build executable: %v: %s", err, output)
	}
	asset := filepath.Join(dir, "ppl.bundle.js")
	data, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	run := func(endpoint, text string) struct {
		Result struct {
			Rows      [][]json.RawMessage `json:"rows"`
			Truncated bool                `json:"truncated"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} {
		t.Helper()
		request := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "execute_query", "params": map[string]any{
			"params": map[string]any{"extra": map[string]string{"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous", "index_pattern": index, "request_timeout_ms": "60000"}},
			"query":  "#!ppl\n" + text, "limit": 1, "page": 1,
		}}
		input, marshalErr := json.Marshal(request)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatalf("start plugin: %v", err)
		}
		if _, err := stdin.Write(append(input, '\n')); err != nil {
			cancel()
			cmd.Wait()
			t.Fatalf("send plugin request: %v", err)
		}
		var response struct {
			Result struct {
				Rows      [][]json.RawMessage `json:"rows"`
				Truncated bool                `json:"truncated"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		decodeErr := json.NewDecoder(stdout).Decode(&response)
		closeErr := stdin.Close()
		waitErr := cmd.Wait()
		if decodeErr != nil || closeErr != nil || waitErr != nil {
			t.Fatalf("execute plugin for %q: decode: %v; close: %v; wait: %v; context: %v; stderr: %s", text, decodeErr, closeErr, waitErr, ctx.Err(), stderr.String())
		}
		return response
	}
	query := "source=" + index + " | fields count"
	missing := run("http://127.0.0.1:1", query)
	if missing.Error == nil || !strings.Contains(missing.Error.Message, "artifact is unavailable") {
		t.Fatalf("missing asset: %+v", missing)
	}
	if err := os.WriteFile(asset, []byte("invalid asset"), 0644); err != nil {
		t.Fatal(err)
	}
	corrupt := run("http://127.0.0.1:1", query)
	if corrupt.Error == nil || !strings.Contains(corrupt.Error.Message, "integrity check failed") {
		t.Fatalf("corrupt asset: %+v", corrupt)
	}
	if err := os.WriteFile(asset, data, 0644); err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{
		"source=" + index + " | ml action='train'",
		"source=" + index + " | where count = [ source=" + index + " | ml action='train' ]",
		"source=" + index + " | head 1000000000 | head 1",
		"source=" + index + " | head 1 from 10001 | head 1",
		"source=other_index | fields count",
		"source=" + index + " | where (",
	} {
		response := run("http://127.0.0.1:1", unsafe)
		if response.Error == nil || !strings.Contains(response.Error.Message, "PPL") {
			t.Fatalf("unsafe PPL was not rejected locally: %q: %+v", unsafe, response)
		}
	}
	if endpoint == "" || os.Getenv("PPL_TEST_INDEX") == "" {
		t.Log("live PPL query skipped: PPL_TEST_ENDPOINT and PPL_TEST_INDEX are required")
		return
	}
	result := run(endpoint, query)
	if result.Error != nil || len(result.Result.Rows) != 1 || !result.Result.Truncated {
		t.Fatalf("PPL result: %+v", result)
	}
}

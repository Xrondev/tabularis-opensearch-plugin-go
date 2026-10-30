package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/opensearch"
	"tabularis-opensearch-plugin-go/internal/ppl"
)

func TestPPLArtifactIntegrityAndCancellation(t *testing.T) {
	asset := os.Getenv("PPL_TEST_BUNDLE")
	if asset == "" {
		t.Skip("PPL_TEST_BUNDLE is required")
	}
	if _, err := ppl.Open(asset + ".missing"); err == nil {
		t.Fatal("missing parser artifact was accepted")
	}
	if _, err := ppl.Open(filepath.Join(filepath.Dir(filepath.Dir(asset)), "bundle.sha256")); err == nil {
		t.Fatal("incorrect parser artifact was accepted")
	}
	validator, err := ppl.Open(asset)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := validator.Validate(ctx, "source=events | fields count", "events", 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled PPL was not stopped: %v", err)
	}
	if err := validator.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := validator.Validate(context.Background(), "source=events | fields count", "events", 10); err == nil {
		t.Fatal("closed PPL validator accepted a query")
	}
}

func TestRealOpenSearchPPL(t *testing.T) {
	endpoint := os.Getenv("PPL_TEST_ENDPOINT")
	index := os.Getenv("PPL_TEST_INDEX")
	asset := os.Getenv("PPL_TEST_BUNDLE")
	if endpoint == "" || index == "" || asset == "" {
		t.Skip("PPL_TEST_ENDPOINT, PPL_TEST_INDEX and PPL_TEST_BUNDLE are required")
	}
	validator, err := ppl.Open(asset)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := validator.Close(); err != nil {
			t.Error(err)
		}
	})
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	operation, err := validator.Validate(ctx, "source="+index+" | fields count | sort count", index, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(operation.Text(), " | head 2") {
		t.Fatal("PPL execution is not bounded")
	}
	raw, err := client.PPL(ctx, operation)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ppl.Normalize(operation, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || len(result.Columns) != 1 || !result.Truncated || result.Pagination != nil || string(result.Rows[0][0]) != "3" {
		encoded, _ := json.Marshal(result)
		t.Fatalf("unexpected PPL result: %s", encoded)
	}
	operation, err = validator.Validate(ctx, "source="+index+" | head 1", index, 5)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = client.PPL(ctx, operation)
	if err != nil {
		t.Fatal(err)
	}
	result, err = ppl.Normalize(operation, raw)
	if err != nil || len(result.Rows) != 1 || result.Truncated {
		t.Fatalf("existing PPL head was not preserved: %+v %v", result, err)
	}
	operation, err = validator.Validate(ctx, "source="+index+" | where count > 3 | stats count()", index, 10)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = client.PPL(ctx, operation)
	if err != nil {
		t.Fatal(err)
	}
	result, err = ppl.Normalize(operation, raw)
	if err != nil || len(result.Rows) != 1 || string(result.Rows[0][0]) != "2" {
		t.Fatalf("PPL filtering and statistics failed: %+v %v", result, err)
	}
	for _, unsafe := range []string{
		"source=" + index + " | ml action='train'",
		"source=" + index + " | where count = 3 | ml action='train'",
		"source=" + index + " | where count = [ source=another | ml action='train' ]",
		"source=" + index + " | where count = 3; DELETE FROM x",
		"source=" + index + " ! | fields count",
		"source=" + index + " | where message = 'unterminated",
		"source=" + index + " | where message = 'x\\n'",
		"source=" + index + " | where count = 3 /* ignored */ | fields count",
		"source=" + index + " | eval x=1",
		"source=" + index + " | head 100",
		"source=" + index + " | head 1000000000 | head 1",
		"source=" + index + " | head 1 from 10001 | head 1",
		"source=" + index + ",another_index | fields count",
		"source=`" + index + "` | fields count",
		"source=" + index + "%2Canother | fields count",
		"source=another_index | fields count",
	} {
		if _, err := validator.Validate(ctx, unsafe, index, 10); err == nil {
			t.Fatalf("unsafe PPL was accepted: %s", unsafe)
		}
	}
	if _, err := validator.Validate(ctx, "source="+index+" | head from 5", index, 1); err == nil {
		t.Fatal("PPL head default size exceeded the page bound")
	}
}

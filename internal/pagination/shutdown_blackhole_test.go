package pagination_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/connectioncache"
	"tabularis-opensearch-plugin-go/internal/pagination"
	"tabularis-opensearch-plugin-go/internal/query"
)

func TestRealShutdownBlackhole(t *testing.T) {
	if os.Getenv("OPENSEARCH_BLACKHOLE_CONFIRMED") != "1" {
		t.Skip("dedicated Docker blackhole fixture is not enabled")
	}
	config, err := connection.Parse(connection.Params{
		ConnectionID: "blackhole",
		Extra: map[string]string{
			"endpoint": "http://127.0.0.1:19202", "deployment": "self_hosted", "auth_mode": "anonymous",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := connectioncache.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Error(err)
		}
	})
	lease, err := cache.Acquire(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lease.Release(); err != nil {
			t.Error(err)
		}
	})
	manager, err := pagination.New(func(err error) { t.Error(err) })
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := json.Marshal(map[string]any{
		"index": ".opensearch-observability",
		"body": map[string]any{
			"query": map[string]any{"match_all": map[string]any{}}, "sort": []string{"_id"}, "_source": false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := query.Parse("#!dsl\n"+string(envelope), 1, 1, config.IndexPattern)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 6 {
		if _, err := manager.Page(context.Background(), lease, "blackhole", fmt.Sprintf("session-%d", i), operation, 1, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := exec.Command("docker", "pause", "tabularis-pit-blackhole-test").Run(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := exec.Command("docker", "unpause", "tabularis-pit-blackhole-test").Run(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = manager.CloseContext(ctx)
	if err == nil {
		t.Fatal("blackholed PIT cleanup unexpectedly succeeded")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("shutdown exceeded the shared deadline")
	}
}

package pagination_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"

	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/connectioncache"
	"tabularis-opensearch-plugin-go/internal/pagination"
	"tabularis-opensearch-plugin-go/internal/query"
)

func TestRealPITPagesAndSessionIsolation(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	fixture := os.Getenv("OPENSEARCH_TEST_FIXTURE_INDEX")
	if endpoint == "" || fixture == "" {
		t.Skip("real OpenSearch endpoint and fixture index are required")
	}
	config, err := connection.Parse(connection.Params{
		ConnectionID: "owner",
		Extra: map[string]string{
			"endpoint":   endpoint,
			"deployment": "self_hosted",
			"auth_mode":  "anonymous",
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
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	envelope, err := json.Marshal(map[string]any{
		"index": fixture,
		"body": map[string]any{
			"query": map[string]any{"match_all": map[string]any{}},
			"sort":  []string{"count"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := query.Parse("#!dsl\n"+string(envelope), 1, 1, config.IndexPattern)
	if err != nil {
		t.Fatal(err)
	}
	for page, count := range []int{3, 4, 5} {
		raw, err := manager.Page(context.Background(), lease, "owner", "session", operation, page+1, true)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Hits struct {
				Hits []struct {
					Source struct {
						Count int `json:"count"`
					} `json:"_source"`
				} `json:"hits"`
			} `json:"hits"`
		}
		if err := json.Unmarshal(raw, &result); err != nil || len(result.Hits.Hits) == 0 || result.Hits.Hits[0].Source.Count != count {
			t.Fatalf("page %d did not return the expected fixture document", page+1)
		}
	}
	if _, err := manager.Page(context.Background(), lease, "another-owner", "session", operation, 2, true); err == nil {
		t.Fatal("different owner accessed a session")
	}
	if _, err := manager.Page(context.Background(), lease, "owner", "session", operation, 2, true); err != nil {
		t.Fatalf("previous page unavailable: %v", err)
	}
	if _, err := manager.Page(context.Background(), lease, "owner", "session", operation, 4, true); err == nil {
		t.Fatal("unavailable page did not fail")
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	changed := config
	changed.PageSize = 50
	replacement, err := cache.Acquire(context.Background(), changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Release(); err != nil {
		t.Fatal(err)
	}
	lease, err = cache.Acquire(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Page(context.Background(), lease, "owner", "session", operation, 2, true); err == nil {
		t.Fatal("replaced connection configuration revived an old session")
	}
	if err := manager.Release("session"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Page(context.Background(), lease, "owner", "session", operation, 2, true); err == nil {
		t.Fatal("released session remained available")
	}
	envelope, err = json.Marshal(map[string]any{
		"index": fixture,
		"body": map[string]any{
			"query": map[string]any{"match_all": map[string]any{}},
			"sort":  []string{"_score"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	duplicateSort, err := query.Parse("#!dsl\n"+string(envelope), 1, 1, config.IndexPattern)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Page(context.Background(), lease, "owner", "duplicates", duplicateSort, 1, true); err == nil {
		t.Fatal("duplicate sort values were not rejected")
	}
	if err := manager.Release("duplicates"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Page(context.Background(), lease, "owner", "cancelled", operation, 1, true); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Page(cancelled, lease, "owner", "cancelled", operation, 2, true); err == nil {
		t.Fatal("cancelled page request succeeded")
	}
	if _, err := manager.Page(context.Background(), lease, "owner", "cancelled", operation, 2, true); err != nil {
		t.Fatalf("session should remain available when a request was cancelled before execution: %v", err)
	}
	if err := manager.Release("cancelled"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"isolated-a", "isolated-b"} {
		if _, err := manager.Page(context.Background(), lease, "owner", id, operation, 1, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Release("isolated-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Page(context.Background(), lease, "owner", "isolated-b", operation, 2, true); err != nil {
		t.Fatalf("releasing one session affected another: %v", err)
	}
	if err := manager.Release("isolated-b"); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	failures := make(chan error, 8)
	for i := range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			id := fmt.Sprintf("parallel-%d", i)
			_, err := manager.Page(context.Background(), lease, "owner", id, operation, 1, true)
			if err == nil {
				err = manager.Release(id)
			}
			failures <- err
		}()
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
}

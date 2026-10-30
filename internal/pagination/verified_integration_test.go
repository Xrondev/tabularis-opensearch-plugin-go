package pagination_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/connectioncache"
	"tabularis-opensearch-plugin-go/internal/pagination"
	"tabularis-opensearch-plugin-go/internal/query"
)

func TestRealVerifiedPages(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	index := os.Getenv("OPENSEARCH_TEST_FIXTURE_INDEX")
	if endpoint == "" || index == "" {
		t.Skip("real OpenSearch endpoint and index are required")
	}
	config, err := connection.Parse(connection.Params{
		ConnectionID: "verified-owner",
		Extra: map[string]string{
			"endpoint": endpoint, "deployment": "self_hosted", "auth_mode": "anonymous",
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
		"index": index,
		"body": map[string]any{
			"query":   map[string]any{"match_all": map[string]any{}},
			"_source": false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := query.Parse("#!dsl\n"+string(envelope), 1, 1, config.IndexPattern)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	identity := "SELECT * FROM verified_fixture LIMIT 3"
	for page := 1; page <= 20; page++ {
		raw, err := manager.VerifiedPage(context.Background(), lease, "verified-owner", "verified-session", operation, page, identity)
		if err != nil {
			t.Fatal(err)
		}
		result, err := query.Normalize(operation, raw)
		if err != nil || result.Pagination == nil {
			t.Fatalf("invalid page result: %v", err)
		}
		for _, row := range result.Rows {
			var id string
			if len(row) < 2 || json.Unmarshal(row[1], &id) != nil || id == "" || seen[id] {
				t.Fatal("page returned a missing or duplicate document ID")
			}
			seen[id] = true
		}
		if page == 1 && result.Pagination.HasMore {
			if _, err := manager.VerifiedPage(context.Background(), lease, "verified-owner", "verified-session", operation, 2, "SELECT * FROM verified_fixture LIMIT 2"); err == nil {
				t.Fatal("changed query identity reused a snapshot")
			}
			if _, err := manager.VerifiedPage(context.Background(), lease, "verified-owner", "verified-session", operation, 2, identity); err == nil {
				t.Fatal("changed query identity retained its old snapshot")
			}
			if _, err := manager.VerifiedPage(context.Background(), lease, "verified-owner", "verified-session", operation, 1, identity); err != nil {
				t.Fatalf("restart after query change failed: %v", err)
			}
		}
		if !result.Pagination.HasMore {
			break
		}
		if page == 20 {
			t.Fatal("fixture exceeded bounded test pages")
		}
	}
	if len(seen) == 0 {
		t.Fatal("expected a nonempty fixture")
	}
	if index == "tabularis_fixture" && len(seen) != 3 {
		t.Fatalf("expected three fixture documents, got %d", len(seen))
	}
	if _, err := manager.Page(context.Background(), lease, "verified-owner", "verified-session", operation, 2, true); err == nil {
		t.Fatal("unverified caller accessed a verified session")
	}
	if err := manager.ReleaseOwned("verified-session", "another-owner"); err == nil {
		t.Fatal("different connection released a session")
	}
	if len(seen) > 1 {
		if _, err := manager.VerifiedPage(context.Background(), lease, "verified-owner", "verified-session", operation, 2, identity); err != nil {
			t.Fatalf("failed release by another owner invalidated the session: %v", err)
		}
	}
	if err := manager.ReleaseOwned("verified-session", ""); err == nil {
		t.Fatal("empty owner released a session")
	}
	if err := manager.ReleaseOwned("verified-session", "verified-owner"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ReleaseOwned("missing-session", "verified-owner"); err != nil {
		t.Fatalf("missing session release failed: %v", err)
	}
}

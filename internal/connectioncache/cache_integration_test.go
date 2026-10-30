package connectioncache_test

import (
	"context"
	"os"
	"sync"
	"testing"

	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/connectioncache"
)

func TestRealConnectionReuseAndIsolation(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("OPENSEARCH_TEST_ENDPOINT is not set")
	}
	config, err := connection.Parse(connection.Params{
		ConnectionID: "connection-a",
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
	first, err := cache.Acquire(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	if err := first.Client().Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := cache.Acquire(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	if first.Client() != second.Client() {
		t.Fatal("same connection and configuration did not reuse its client")
	}
	var workers sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			lease, err := cache.Acquire(context.Background(), config)
			if err == nil {
				err = lease.Client().Test(context.Background())
				if releaseErr := lease.Release(); err == nil {
					err = releaseErr
				}
			}
			errors <- err
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	changed := config
	changed.PageSize = 50
	replacement, err := cache.Acquire(context.Background(), changed)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Release()
	if first.Client() == replacement.Client() {
		t.Fatal("changed configuration reused the previous client")
	}
	if err := replacement.Client().Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	other := config
	other.ConnectionID = "connection-b"
	different, err := cache.Acquire(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	defer different.Release()
	if first.Client() == different.Client() {
		t.Fatal("independent connections shared a client")
	}
	other.ConnectionID = ""
	unsaved, err := cache.Acquire(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	defer unsaved.Release()
	if first.Client() == unsaved.Client() || different.Client() == unsaved.Client() {
		t.Fatal("unsaved connection shared a client")
	}
}

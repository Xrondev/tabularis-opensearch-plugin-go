package opensearch

import (
	"context"
	"os"
	"testing"
	"time"

	"tabularis-opensearch-plugin-go/internal/awsauth"
	"tabularis-opensearch-plugin-go/internal/connection"
)

func TestMatchesIndexPattern(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		index   string
		matches bool
		invalid bool
	}{
		{"*", "events-2026", true, false},
		{"events-*", "events-2026", true, false},
		{"events-*", "other", false, false},
		{"first-*,second-*", "second-1", true, false},
		{"first-*,[", "first-1", false, true},
	} {
		matches, err := matchesIndexPattern(tc.pattern, tc.index)
		if matches != tc.matches || (err != nil) != tc.invalid {
			t.Errorf("matchesIndexPattern(%q, %q) = %v, %v", tc.pattern, tc.index, matches, err)
		}
	}
}

func TestRealServerlessIndexDiscovery(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_SERVERLESS_TEST_ENDPOINT")
	profile := os.Getenv("OPENSEARCH_SERVERLESS_TEST_PROFILE")
	region := os.Getenv("OPENSEARCH_SERVERLESS_TEST_REGION")
	if endpoint == "" || profile == "" || region == "" {
		t.Skip("serverless endpoint, profile and region are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	signer, err := awsauth.New(ctx, awsauth.Options{Deployment: "aws_serverless", Region: region, AuthMode: "aws_profile", Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	config, err := connection.Parse(connection.Params{Extra: map[string]string{
		"endpoint": endpoint, "deployment": "aws_serverless", "auth_mode": "aws_profile", "aws_profile": profile, "aws_region": region,
	}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(config, signer)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Test(ctx); err != nil {
		t.Fatal(err)
	}
	tables, err := client.Tables(ctx)
	if err != nil || len(tables) == 0 {
		t.Fatalf("expected at least one accessible index: %v", err)
	}
	client.config.IndexPattern = tables[0].Name
	filtered, err := client.Tables(ctx)
	if err != nil || len(filtered) != 1 || filtered[0].Name != tables[0].Name {
		t.Fatalf("exact index filter returned unexpected results: %v", err)
	}
	client.config.IndexPattern = "no-such-index-verified-8e2a*"
	filtered, err = client.Tables(ctx)
	if err != nil || len(filtered) != 0 {
		t.Fatalf("nonmatching index filter returned unexpected results: %v", err)
	}
}

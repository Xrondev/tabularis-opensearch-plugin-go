package opensearch

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"tabularis-opensearch-plugin-go/internal/awsauth"
	"tabularis-opensearch-plugin-go/internal/connection"
)

func TestRealAWSConnectionTestDenied(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_DENIED_TEST_ENDPOINT")
	profile := os.Getenv("OPENSEARCH_DENIED_TEST_PROFILE")
	region := os.Getenv("OPENSEARCH_DENIED_TEST_REGION")
	deployment := os.Getenv("OPENSEARCH_DENIED_TEST_DEPLOYMENT")
	if endpoint == "" || profile == "" || region == "" || deployment == "" {
		t.Skip("denied AWS test endpoint, profile, region and deployment are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	signer, err := awsauth.New(ctx, awsauth.Options{Deployment: deployment, Region: region, AuthMode: "aws_profile", Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	config, err := connection.Parse(connection.Params{Extra: map[string]string{
		"endpoint": endpoint, "deployment": deployment, "auth_mode": "aws_profile", "aws_profile": profile, "aws_region": region,
	}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(config, signer)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	err = client.Test(ctx)
	if err == nil || !strings.Contains(err.Error(), "could not list indices") || !strings.Contains(err.Error(), "index-listing access") || !strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), endpoint) || strings.Contains(err.Error(), profile) {
		t.Fatalf("unexpected connection-test error: %v", err)
	}
}

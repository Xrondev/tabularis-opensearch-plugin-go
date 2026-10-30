package awsdiscovery

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDiscoverRequiresValidConfiguration(t *testing.T) {
	for _, opts := range []Options{
		{},
		{Deployment: "self_hosted", Region: "us-east-1", Profile: "default"},
		{Deployment: "aws_managed", Region: "", Profile: "default"},
		{Deployment: "aws_serverless", Region: "us-east-1", Profile: ""},
		{Deployment: "aws_managed", Region: " us-east-1", Profile: "default"},
	} {
		_, err := Discover(context.Background(), opts)
		var discoveryErr *Error
		if !errors.As(err, &discoveryErr) || discoveryErr.Kind != "invalid_configuration" {
			t.Fatalf("configuration was not rejected: %#v: %v", opts, err)
		}
	}
}

func TestEndpointValidation(t *testing.T) {
	for _, test := range []struct {
		endpoint string
		valid    bool
	}{
		{"https://search.example.com", true},
		{"http://search.example.com", false},
		{"https://user:secret@search.example.com", false},
		{"https://search.example.com/path", false},
		{"https://search.example.com?credential=secret", false},
	} {
		if validEndpoint(test.endpoint) != test.valid {
			t.Fatalf("unexpected endpoint validation for %q", test.endpoint)
		}
	}
}

func TestLiveReadOnlyDiscovery(t *testing.T) {
	profile, region := os.Getenv("AWS_DISCOVERY_TEST_PROFILE"), os.Getenv("AWS_DISCOVERY_TEST_REGION")
	if profile == "" || region == "" {
		t.Skip("AWS_DISCOVERY_TEST_PROFILE and AWS_DISCOVERY_TEST_REGION are required")
	}
	for _, deployment := range []string{"aws_managed", "aws_serverless"} {
		t.Run(deployment, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			resources, err := Discover(ctx, Options{Deployment: deployment, Region: region, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			for _, resource := range resources {
				if resource.Name == "" || resource.Type == "" || !validEndpoint(resource.Endpoint) || !strings.HasPrefix(resource.Endpoint, "https://") {
					t.Fatal("discovery returned an invalid resource")
				}
			}
			t.Logf("discovered %d resources", len(resources))
		})
	}
}

package awsauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"

	"tabularis-opensearch-plugin-go/internal/awsauth"
)

func TestStaticCredentialsSignRequests(t *testing.T) {
	for _, tc := range []struct {
		deployment string
		service    string
	}{
		{"aws_managed", "es"},
		{"aws_serverless", "aoss"},
	} {
		t.Run(tc.deployment, func(t *testing.T) {
			s, err := awsauth.New(context.Background(), awsauth.Options{
				Deployment:      tc.deployment,
				Region:          "us-east-1",
				AuthMode:        "aws_static",
				AccessKeyID:     "AKIDEXAMPLE",
				SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
				SessionToken:    "test-session-token",
			})
			if err != nil {
				t.Fatal(err)
			}
			body := `{"query":{"match_all":{}}}`
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.us-east-1.amazonaws.com/index/_search", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SignRequest(req); err != nil {
				t.Fatal(err)
			}
			authorization := req.Header.Get("Authorization")
			if !strings.Contains(authorization, "/us-east-1/"+tc.service+"/aws4_request") {
				t.Fatalf("incorrect signing scope: %q", authorization)
			}
			digest := sha256.Sum256([]byte(body))
			if got := req.Header.Get("X-Amz-Content-Sha256"); got != hex.EncodeToString(digest[:]) {
				t.Fatalf("incorrect payload hash: %q", got)
			}
			if req.Header.Get("X-Amz-Security-Token") != "test-session-token" {
				t.Fatal("session token was not signed")
			}
			got, err := io.ReadAll(req.Body)
			if err != nil || string(got) != body {
				t.Fatalf("request body not preserved: %q, %v", got, err)
			}
		})
	}
}

func TestInvalidCredentialConfiguration(t *testing.T) {
	for _, options := range []awsauth.Options{
		{Deployment: "aws_managed", AuthMode: "aws_default"},
		{Deployment: "self_hosted", Region: "us-east-1", AuthMode: "aws_default"},
		{Deployment: "aws_serverless", Region: "us-east-1", AuthMode: "aws_profile"},
		{Deployment: "aws_managed", Region: "us-east-1", AuthMode: "aws_static", AccessKeyID: "id"},
		{Deployment: "aws_managed", Region: "us-east-1", AuthMode: "aws_default", Profile: "unexpected"},
		{Deployment: "aws_managed", Region: "us-east-1", AuthMode: "aws_profile", Profile: "profile", AccessKeyID: "unexpected"},
	} {
		if _, err := awsauth.New(context.Background(), options); err == nil {
			t.Errorf("accepted invalid credential configuration: deployment=%q mode=%q", options.Deployment, options.AuthMode)
		}
	}
}

func TestServerlessEmptyPayloadHash(t *testing.T) {
	s, err := awsauth.New(context.Background(), awsauth.Options{
		Deployment:      "aws_serverless",
		Region:          "us-west-2",
		AuthMode:        "aws_static",
		AccessKeyID:     "AKIDEXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.us-west-2.aoss.amazonaws.com/index/_mapping", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SignRequest(req); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("X-Amz-Content-Sha256"); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("incorrect empty payload hash: %q", got)
	}
}

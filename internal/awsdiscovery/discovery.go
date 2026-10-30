package awsdiscovery

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	managed "github.com/aws/aws-sdk-go-v2/service/opensearch"
	managedtypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	serverless "github.com/aws/aws-sdk-go-v2/service/opensearchserverless"
	serverlesstypes "github.com/aws/aws-sdk-go-v2/service/opensearchserverless/types"
	"github.com/aws/smithy-go"
)

type Options struct {
	Deployment string `json:"deployment"`
	Region     string `json:"region"`
	Profile    string `json:"profile"`
}

type Resource struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Type     string `json:"type"`
}

type Error struct {
	Kind string
}

func (e *Error) Error() string {
	switch e.Kind {
	case "invalid_configuration":
		return "AWS discovery requires an AWS deployment, region, and profile"
	case "credentials":
		return "AWS profile credentials are unavailable or expired"
	case "permission":
		return "AWS discovery permission denied; check list and describe permissions"
	case "cancelled":
		return "AWS discovery was cancelled or timed out"
	case "incomplete":
		return "AWS discovery returned incomplete resource details"
	default:
		return "AWS discovery request failed; check the region and network"
	}
}

func classify(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: "cancelled"}
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "AccessDenied", "AccessDeniedException", "UnauthorizedException", "ForbiddenException":
			return &Error{Kind: "permission"}
		case "ExpiredToken", "ExpiredTokenException", "InvalidClientTokenId", "UnrecognizedClientException":
			return &Error{Kind: "credentials"}
		}
	}
	return &Error{Kind: "request"}
}

func Discover(ctx context.Context, opts Options) ([]Resource, error) {
	if (opts.Deployment != "aws_managed" && opts.Deployment != "aws_serverless") || opts.Region == "" || opts.Region != strings.TrimSpace(opts.Region) || opts.Profile == "" || opts.Profile != strings.TrimSpace(opts.Profile) {
		return nil, &Error{Kind: "invalid_configuration"}
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(opts.Region), config.WithSharedConfigProfile(opts.Profile))
	if err != nil {
		return nil, &Error{Kind: "credentials"}
	}
	if _, err = cfg.Credentials.Retrieve(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, &Error{Kind: "cancelled"}
		}
		return nil, &Error{Kind: "credentials"}
	}
	var resources []Resource
	if opts.Deployment == "aws_managed" {
		resources, err = discoverManaged(ctx, managed.NewFromConfig(cfg))
	} else {
		resources, err = discoverServerless(ctx, serverless.NewFromConfig(cfg))
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Name != resources[j].Name {
			return resources[i].Name < resources[j].Name
		}
		return resources[i].Endpoint < resources[j].Endpoint
	})
	return resources, nil
}

func validEndpoint(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}

func discoverManaged(ctx context.Context, client *managed.Client) ([]Resource, error) {
	listed, err := client.ListDomainNames(ctx, &managed.ListDomainNamesInput{EngineType: managedtypes.EngineTypeOpenSearch})
	if err != nil {
		return nil, classify(err)
	}
	resources := make([]Resource, 0, len(listed.DomainNames))
	for _, domain := range listed.DomainNames {
		name := aws.ToString(domain.DomainName)
		if name == "" {
			return nil, &Error{Kind: "incomplete"}
		}
		detail, err := client.DescribeDomains(ctx, &managed.DescribeDomainsInput{DomainNames: []string{name}})
		if err != nil {
			return nil, classify(err)
		}
		if len(detail.DomainStatusList) != 1 {
			return nil, &Error{Kind: "incomplete"}
		}
		status := detail.DomainStatusList[0]
		if aws.ToString(status.DomainName) != name {
			return nil, &Error{Kind: "incomplete"}
		}
		endpoints := []string{aws.ToString(status.Endpoint), aws.ToString(status.EndpointV2)}
		for _, endpoint := range status.Endpoints {
			endpoints = append(endpoints, endpoint)
		}
		seen := make(map[string]bool)
		for _, endpoint := range endpoints {
			if endpoint == "" {
				continue
			}
			endpoint = "https://" + endpoint
			if !validEndpoint(endpoint) {
				return nil, &Error{Kind: "incomplete"}
			}
			if !seen[endpoint] {
				resources = append(resources, Resource{Name: name, Endpoint: endpoint, Type: "managed"})
				seen[endpoint] = true
			}
		}
	}
	return resources, nil
}

func discoverServerless(ctx context.Context, client *serverless.Client) ([]Resource, error) {
	input := &serverless.ListCollectionsInput{CollectionFilters: &serverlesstypes.CollectionFilters{Status: serverlesstypes.CollectionStatusActive}}
	var ids []string
	seenTokens := make(map[string]bool)
	for {
		page, err := client.ListCollections(ctx, input)
		if err != nil {
			return nil, classify(err)
		}
		for _, collection := range page.CollectionSummaries {
			id := aws.ToString(collection.Id)
			if id == "" {
				return nil, &Error{Kind: "incomplete"}
			}
			ids = append(ids, id)
		}
		token := aws.ToString(page.NextToken)
		if token == "" {
			break
		}
		if seenTokens[token] {
			return nil, &Error{Kind: "incomplete"}
		}
		seenTokens[token] = true
		input.NextToken = aws.String(token)
	}
	resources := make([]Resource, 0, len(ids))
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		detail, err := client.BatchGetCollection(ctx, &serverless.BatchGetCollectionInput{Ids: ids[start:end]})
		if err != nil {
			return nil, classify(err)
		}
		if len(detail.CollectionErrorDetails) != 0 || len(detail.CollectionDetails) != end-start {
			return nil, &Error{Kind: "incomplete"}
		}
		for _, collection := range detail.CollectionDetails {
			name, endpoint := aws.ToString(collection.Name), aws.ToString(collection.CollectionEndpoint)
			if name == "" || !validEndpoint(endpoint) {
				return nil, &Error{Kind: "incomplete"}
			}
			resources = append(resources, Resource{Name: name, Endpoint: endpoint, Type: string(collection.Type)})
		}
	}
	return resources, nil
}

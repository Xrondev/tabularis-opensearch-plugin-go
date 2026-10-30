package awsauth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/opensearch-project/opensearch-go/v5/signer"
	"github.com/opensearch-project/opensearch-go/v5/signer/awsv2"
)

type Options struct {
	Deployment      string
	Region          string
	AuthMode        string
	Profile         string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

func New(ctx context.Context, opts Options) (signer.Signer, error) {
	if strings.TrimSpace(opts.Region) == "" || opts.Region != strings.TrimSpace(opts.Region) {
		return nil, errors.New("AWS region is required")
	}

	var service string
	switch opts.Deployment {
	case "aws_managed":
		service = "es"
	case "aws_serverless":
		service = "aoss"
	default:
		return nil, errors.New("AWS signing requires an AWS deployment")
	}

	hasStatic := opts.AccessKeyID != "" || opts.SecretAccessKey != "" || opts.SessionToken != ""
	loadOptions := []func(*config.LoadOptions) error{config.WithRegion(opts.Region)}
	switch opts.AuthMode {
	case "aws_default":
		if opts.Profile != "" || hasStatic {
			return nil, errors.New("default AWS authentication cannot include profile or static credentials")
		}
	case "aws_profile":
		if strings.TrimSpace(opts.Profile) == "" || opts.Profile != strings.TrimSpace(opts.Profile) || hasStatic {
			return nil, errors.New("AWS profile authentication requires a profile and excludes static credentials")
		}
		loadOptions = append(loadOptions, config.WithSharedConfigProfile(opts.Profile))
	case "aws_static":
		if opts.Profile != "" || strings.TrimSpace(opts.AccessKeyID) == "" || strings.TrimSpace(opts.SecretAccessKey) == "" {
			return nil, errors.New("static AWS authentication requires an access key and secret key and excludes profile")
		}
		provider := credentials.NewStaticCredentialsProvider(opts.AccessKeyID, opts.SecretAccessKey, opts.SessionToken)
		loadOptions = append(loadOptions, config.WithCredentialsProvider(provider))
	default:
		return nil, errors.New("unsupported AWS authentication mode")
	}

	awsConfig, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	return awsv2.NewSignerWithService(awsConfig, service)
}

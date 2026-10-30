package connection

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

type Params struct {
	ConnectionID string            `json:"connection_id"`
	Username     string            `json:"username"`
	Password     string            `json:"password"`
	SSLCA        string            `json:"ssl_ca"`
	Extra        map[string]string `json:"extra"`
}

type Config struct {
	ConnectionID        string
	Endpoint            string
	Deployment          string
	AuthMode            string
	Username            string
	Password            string
	CAPath              string
	Region              string
	Profile             string
	AccessKeyID         string
	SecretAccessKey     string
	SessionToken        string
	IndexPattern        string
	PaginationMode      string
	VerifiedExactFields []string
	Timeout             time.Duration
	PageSize            int
}

func Parse(params Params) (Config, error) {
	extra := params.Extra
	config := Config{
		ConnectionID:    params.ConnectionID,
		Endpoint:        extra["endpoint"],
		Deployment:      extra["deployment"],
		AuthMode:        extra["auth_mode"],
		Username:        params.Username,
		Password:        params.Password,
		CAPath:          params.SSLCA,
		Region:          extra["aws_region"],
		Profile:         extra["aws_profile"],
		AccessKeyID:     extra["aws_access_key_id"],
		SecretAccessKey: extra["aws_secret_access_key"],
		SessionToken:    extra["aws_session_token"],
		IndexPattern:    extra["index_pattern"],
		PaginationMode:  extra["pagination_mode"],
		Timeout:         30 * time.Second,
		PageSize:        100,
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint == nil || endpoint.Hostname() == "" || endpoint.Opaque != "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return Config{}, errors.New("endpoint must be an absolute HTTP or HTTPS URL without credentials, query, or fragment")
	}
	if endpoint.RawPath != "" || (endpoint.Path != "" && endpoint.Path != "/" && path.Clean(endpoint.Path) != strings.TrimSuffix(endpoint.Path, "/")) || strings.Contains(endpoint.Path, "\\") || strings.Contains(endpoint.Path, "..") {
		return Config{}, errors.New("endpoint base path is invalid")
	}
	if port := endpoint.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return Config{}, errors.New("endpoint port is invalid")
		}
	}
	config.Endpoint = strings.TrimSuffix(endpoint.String(), "/")
	switch config.Deployment {
	case "self_hosted":
		if config.AuthMode != "anonymous" && config.AuthMode != "basic" {
			return Config{}, errors.New("unsupported self-hosted authentication mode")
		}
	case "aws_managed", "aws_serverless":
		if endpoint.Scheme != "https" {
			return Config{}, errors.New("AWS endpoints require HTTPS")
		}
		if strings.TrimSpace(config.Region) == "" {
			return Config{}, errors.New("AWS region is required")
		}
		if config.AuthMode != "aws_default" && config.AuthMode != "aws_profile" && config.AuthMode != "aws_static" && !(config.Deployment == "aws_managed" && config.AuthMode == "basic") {
			return Config{}, errors.New("unsupported AWS authentication mode")
		}
	default:
		return Config{}, errors.New("deployment must be self_hosted, aws_managed, or aws_serverless")
	}
	if config.AuthMode == "basic" && (config.Username == "" || config.Password == "") {
		return Config{}, errors.New("basic authentication requires username and password")
	}
	if config.AuthMode != "basic" && (config.Username != "" || config.Password != "") {
		return Config{}, errors.New("username and password require basic authentication")
	}
	staticSet := config.AccessKeyID != "" || config.SecretAccessKey != "" || config.SessionToken != ""
	if config.AuthMode == "aws_static" {
		if config.AccessKeyID == "" || config.SecretAccessKey == "" || config.Profile != "" {
			return Config{}, errors.New("static AWS authentication requires access key and secret, without profile")
		}
	} else if staticSet {
		return Config{}, errors.New("AWS static credentials require aws_static authentication")
	}
	if config.AuthMode == "aws_profile" {
		if strings.TrimSpace(config.Profile) == "" {
			return Config{}, errors.New("AWS profile is required")
		}
	} else if config.Profile != "" {
		return Config{}, errors.New("AWS profile requires aws_profile authentication")
	}
	if config.IndexPattern == "" {
		config.IndexPattern = "*"
	}
	if config.PaginationMode == "" {
		config.PaginationMode = "offset"
	}
	if config.PaginationMode != "offset" && config.PaginationMode != "pit" {
		return Config{}, errors.New("pagination_mode must be offset or pit")
	}
	if value := extra["verified_exact_fields"]; value != "" {
		fields := strings.Split(value, ",")
		if len(fields) > 100 {
			return Config{}, errors.New("verified_exact_fields must contain at most 100 field names")
		}
		seen := make(map[string]bool, len(fields))
		for _, field := range fields {
			field = strings.TrimSpace(field)
			if field == "" || seen[field] {
				return Config{}, errors.New("verified_exact_fields must contain unique top-level field names")
			}
			for _, c := range field {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
					return Config{}, errors.New("verified_exact_fields contains an invalid top-level field name")
				}
			}
			seen[field] = true
			config.VerifiedExactFields = append(config.VerifiedExactFields, field)
		}
	}
	if strings.ContainsAny(config.IndexPattern, "/\\?#%") || strings.Contains(config.IndexPattern, "..") {
		return Config{}, errors.New("index pattern contains invalid path characters")
	}
	if value := extra["request_timeout_ms"]; value != "" {
		ms, err := strconv.Atoi(value)
		if err != nil || ms < 1000 || ms > 300000 {
			return Config{}, fmt.Errorf("request_timeout_ms must be an integer from 1000 to 300000")
		}
		config.Timeout = time.Duration(ms) * time.Millisecond
	}
	if value := extra["page_size"]; value != "" {
		size, err := strconv.Atoi(value)
		if err != nil || size < 1 || size > 1000 {
			return Config{}, fmt.Errorf("page_size must be an integer from 1 to 1000")
		}
		config.PageSize = size
	}
	return config, nil
}

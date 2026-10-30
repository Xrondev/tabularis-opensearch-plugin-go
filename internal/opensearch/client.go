package opensearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	client "github.com/opensearch-project/opensearch-go/v5"
	"github.com/opensearch-project/opensearch-go/v5/signer"

	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/ppl"
	"tabularis-opensearch-plugin-go/internal/query"
	"tabularis-opensearch-plugin-go/internal/sqlquery"
)

const maxResponseBytes = 32 * 1024 * 1024

type Client struct {
	client    *client.Client
	transport *http.Transport
	config    connection.Config
}

type Table struct {
	Name    string  `json:"name"`
	Comment *string `json:"comment,omitempty"`
}

type Column struct {
	Name            string `json:"name"`
	DataType        string `json:"data_type"`
	IsPK            bool   `json:"is_pk"`
	IsNullable      bool   `json:"is_nullable"`
	IsAutoIncrement bool   `json:"is_auto_increment"`
	FullType        string `json:"full_type,omitempty"`
	Comment         string `json:"comment,omitempty"`
}

func New(config connection.Config, requestSigner signer.Signer) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if config.CAPath != "" {
		certificates, err := os.ReadFile(config.CAPath)
		if err != nil {
			return nil, fmt.Errorf("read CA certificate: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system certificates: %w", err)
		}
		if !roots.AppendCertsFromPEM(certificates) {
			return nil, errors.New("CA certificate contains no valid PEM certificates")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	if (config.AuthMode == "aws_default" || config.AuthMode == "aws_profile" || config.AuthMode == "aws_static") && requestSigner == nil {
		return nil, errors.New("AWS authentication requires a request signer")
	}
	discover := false
	api, err := client.NewClient(client.Config{
		Addresses:             []string{config.Endpoint},
		Username:              config.Username,
		Password:              config.Password,
		Signer:                requestSigner,
		Transport:             transport,
		DiscoverNodesOnStart:  &discover,
		DiscoverNodesInterval: -time.Second,
		NodeStatsInterval:     -time.Second,
		DisableRetry:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("create OpenSearch client: %w", err)
	}
	return &Client{client: api, transport: transport, config: config}, nil
}

func (c *Client) Close() error {
	c.transport.CloseIdleConnections()
	return c.client.Close()
}

func (c *Client) PPL(ctx context.Context, operation ppl.Query) (json.RawMessage, error) {
	if err := operation.Validate(); err != nil {
		return nil, err
	}
	index := operation.Index()
	allowed := false
	for _, pattern := range strings.Split(c.config.IndexPattern, ",") {
		if index == pattern {
			allowed = true
			break
		}
		if strings.Contains(index, "*") {
			continue
		}
		matched, err := path.Match(pattern, index)
		if err != nil {
			return nil, errors.New("invalid index pattern")
		}
		allowed = allowed || matched
	}
	if !allowed {
		return nil, errors.New("PPL index is outside the configured scope")
	}
	body, err := json.Marshal(struct {
		Query string `json:"query"`
	}{Query: operation.Text()})
	if err != nil || len(body) > 8*1024*1024 {
		return nil, errors.New("PPL request is invalid or exceeds the size limit")
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "/_plugins/_ppl", bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("create PPL request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Transport.Stream(request)
	if err != nil {
		return nil, transportFailure(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, httpFailure(response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, transportFailure(ctx, err)
	}
	if len(data) > maxResponseBytes || !json.Valid(data) {
		return nil, errors.New("PPL response is invalid or exceeds the size limit")
	}
	return json.RawMessage(data), nil
}

func (c *Client) SQL(ctx context.Context, operation sqlquery.Query) (json.RawMessage, error) {
	if err := operation.Validate(); err != nil {
		return nil, err
	}
	index := operation.Index()
	allowed := false
	for _, pattern := range strings.Split(c.config.IndexPattern, ",") {
		if index == pattern {
			allowed = true
			break
		}
		if strings.Contains(index, "*") {
			continue
		}
		matched, err := path.Match(pattern, index)
		if err != nil {
			return nil, errors.New("invalid index pattern")
		}
		allowed = allowed || matched
	}
	if !allowed {
		return nil, errors.New("index is outside the configured index pattern")
	}
	body, err := json.Marshal(struct {
		Query string `json:"query"`
	}{Query: operation.SQL()})
	if err != nil {
		return nil, errors.New("encode SQL request")
	}
	if len(body) > 8*1024*1024 {
		return nil, errors.New("request body exceeded the size limit")
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "/_plugins/_sql", bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("create SQL request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Transport.Stream(request)
	if err != nil {
		return nil, transportFailure(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, httpFailure(response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, transportFailure(ctx, err)
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("SQL response exceeded the size limit")
	}
	if !json.Valid(data) {
		return nil, errors.New("SQL response is not valid JSON")
	}
	var cursor struct {
		Cursor string `json:"cursor"`
	}
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, errors.New("invalid SQL response")
	}
	if cursor.Cursor != "" {
		if err := c.closeSQLCursor(cursor.Cursor); err != nil {
			return nil, fmt.Errorf("SQL cursor pagination is not supported; cursor cleanup failed: %w", err)
		}
		return nil, errors.New("SQL cursor pagination is not supported; cursor was closed")
	}
	return json.RawMessage(data), nil
}

func (c *Client) closeSQLCursor(cursor string) error {
	body, err := json.Marshal(struct {
		Cursor string `json:"cursor"`
	}{Cursor: cursor})
	if err != nil || len(body) > 8*1024*1024 {
		return errors.New("invalid SQL cursor")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "/_plugins/_sql/close", bytes.NewReader(body))
	if err != nil {
		return errors.New("create SQL cursor cleanup request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Transport.Stream(request)
	if err != nil {
		return transportFailure(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpFailure(response.StatusCode)
	}
	var result struct {
		Succeeded bool `json:"succeeded"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&result); err != nil || !result.Succeeded {
		return errors.New("SQL cursor cleanup was not confirmed")
	}
	return nil
}

func (c *Client) Do(ctx context.Context, operation query.Request) (json.RawMessage, error) {
	if err := operation.Validate(); err != nil {
		return nil, err
	}
	index := operation.Index()
	if index == "" || strings.ContainsAny(index, "/\\?#%[] ,") || strings.Contains(index, "..") {
		return nil, errors.New("invalid index or alias name")
	}
	allowed := false
	for _, pattern := range strings.Split(c.config.IndexPattern, ",") {
		if index == pattern {
			allowed = true
			break
		}
		if strings.Contains(index, "*") {
			continue
		}
		matched, err := path.Match(pattern, index)
		if err != nil {
			return nil, errors.New("invalid index pattern")
		}
		allowed = allowed || matched
	}
	if !allowed {
		return nil, errors.New("index is outside the configured index pattern")
	}
	endpoint := operation.Path()
	method := operation.Method()
	valid := method == http.MethodPost && endpoint == "/"+index+"/_search"
	if method == http.MethodGet {
		for _, suffix := range []string{"/_mapping", "/_settings", "/_alias", "/_stats"} {
			valid = valid || endpoint == "/"+index+suffix
		}
	}
	parsed, parseErr := url.ParseRequestURI(endpoint)
	if parseErr != nil || parsed.IsAbs() || parsed.Host != "" {
		return nil, errors.New("invalid operation path")
	}
	segments := strings.Split(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/")
	if len(segments) >= 2 && segments[0] == index {
		switch operation.Kind() {
		case "count":
			valid = len(segments) == 2 && segments[1] == "_count" && parsed.RawQuery == "" && (method == http.MethodGet || method == http.MethodPost)
		case "field_caps":
			valid = len(segments) == 2 && segments[1] == "_field_caps" && method == http.MethodGet
		case "document":
			valid = len(segments) == 3 && segments[1] == "_doc" && method == http.MethodGet
		case "explain":
			valid = len(segments) == 3 && segments[1] == "_explain" && method == http.MethodPost
		}
	}
	if !valid {
		return nil, errors.New("operation is not permitted")
	}
	body := operation.Body()
	if len(body) > 8*1024*1024 {
		return nil, errors.New("request body exceeded the size limit")
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create OpenSearch request: %w", err)
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Transport.Stream(request)
	if err != nil {
		return nil, transportFailure(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, httpFailure(response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, transportFailure(ctx, err)
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("OpenSearch response exceeded the size limit")
	}
	if !json.Valid(data) {
		return nil, errors.New("OpenSearch response is not valid JSON")
	}
	return json.RawMessage(data), nil
}

func (c *Client) Test(ctx context.Context) error {
	_, err := c.Tables(ctx)
	return err
}

func (c *Client) Tables(ctx context.Context) ([]Table, error) {
	query := url.Values{"format": {"json"}, "h": {"index"}, "expand_wildcards": {"open"}}
	var rows []struct {
		Index string `json:"index"`
	}
	if err := c.get(ctx, "/_cat/indices/"+url.PathEscape(c.config.IndexPattern), query, &rows); err != nil {
		return nil, err
	}
	tables := make([]Table, 0, len(rows))
	for _, row := range rows {
		if row.Index == "" {
			return nil, errors.New("index listing returned an empty name")
		}
		tables = append(tables, Table{Name: row.Index})
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return tables, nil
}

func (c *Client) Columns(ctx context.Context, index string) ([]Column, error) {
	if index == "" || strings.ContainsAny(index, "/\\?#%*[]") || strings.Contains(index, "..") {
		return nil, errors.New("invalid index or alias name")
	}
	allowed := false
	for _, pattern := range strings.Split(c.config.IndexPattern, ",") {
		matched, err := path.Match(pattern, index)
		if err != nil {
			return nil, errors.New("invalid index pattern")
		}
		allowed = allowed || matched
	}
	if !allowed {
		return nil, errors.New("index is outside the configured index pattern")
	}
	var response map[string]struct {
		Mappings struct {
			Properties map[string]field `json:"properties"`
		} `json:"mappings"`
	}
	if err := c.get(ctx, "/"+url.PathEscape(index)+"/_mapping", nil, &response); err != nil {
		return nil, err
	}
	columns := make(map[string]Column)
	for _, indexMapping := range response {
		if err := addFields(columns, "", indexMapping.Mappings.Properties, false); err != nil {
			return nil, err
		}
	}
	result := make([]Column, 0, len(columns))
	for _, column := range columns {
		result = append(result, column)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

type field struct {
	Type       string           `json:"type"`
	Properties map[string]field `json:"properties"`
	Fields     map[string]field `json:"fields"`
}

func addFields(columns map[string]Column, prefix string, properties map[string]field, multi bool) error {
	for name, definition := range properties {
		if strings.Contains(name, ".") {
			return errors.New("mapping contains a dotted field name requiring explicit path encoding")
		}
		fullName := name
		if prefix != "" {
			fullName = prefix + "." + name
		}
		typeName := definition.Type
		if typeName == "" {
			typeName = "object"
		}
		comment := "source field"
		if multi {
			comment = "multi-field"
		}
		column := Column{Name: fullName, DataType: typeName, FullType: typeName, Comment: comment, IsNullable: true}
		if existing, ok := columns[fullName]; ok && existing != column {
			return fmt.Errorf("incompatible mappings for field %q", fullName)
		}
		columns[fullName] = column
		if err := addFields(columns, fullName, definition.Properties, false); err != nil {
			return err
		}
		if err := addFields(columns, fullName, definition.Fields, true); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) get(ctx context.Context, endpoint string, query url.Values, target any) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create OpenSearch request: %w", err)
	}
	request.URL.RawQuery = query.Encode()
	response, err := c.client.Transport.Stream(request)
	if err != nil {
		return transportFailure(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpFailure(response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return transportFailure(ctx, err)
	}
	if len(data) > maxResponseBytes {
		return errors.New("OpenSearch response exceeded the size limit")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode OpenSearch response: %w", err)
	}
	return nil
}

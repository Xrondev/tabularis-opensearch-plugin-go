package query

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Request struct {
	method    string
	path      string
	index     string
	body      []byte
	search    bool
	size      int
	valid     bool
	kind      string
	page      int
	windowEnd bool
}

func (r Request) Method() string { return r.method }
func (r Request) Path() string   { return r.path }
func (r Request) Index() string  { return r.index }
func (r Request) Body() []byte   { return bytes.Clone(r.body) }
func (r Request) Kind() string   { return r.kind }

func (r Request) Validate() error {
	if !r.valid {
		return errors.New("invalid or uninitialized query request")
	}
	return nil
}

func Parse(text string, pageSize, page int, indexPattern string) (Request, error) {
	if page < 1 {
		return Request{}, errors.New("page must be at least one")
	}
	if pageSize < 1 || pageSize > 1000 {
		return Request{}, errors.New("page size must be between 1 and 1000")
	}
	text = strings.TrimSpace(text)
	mode, body, found := strings.Cut(text, "\n")
	if !found {
		return Request{}, errors.New("query requires a mode directive and body")
	}
	mode = strings.TrimSpace(mode)
	switch mode {
	case "#!dsl":
		return parseDSL([]byte(body), pageSize, page, indexPattern)
	case "#!rest":
		return parseREST(body, pageSize, page, indexPattern)
	default:
		return Request{}, errors.New("query mode is not supported")
	}
}

func parseDSL(data []byte, pageSize, page int, pattern string) (Request, error) {
	object, err := jsonObject(data)
	if err != nil {
		return Request{}, err
	}
	if len(object) != 2 {
		return Request{}, errors.New("DSL envelope requires only index and body")
	}
	var index string
	if err := json.Unmarshal(object["index"], &index); err != nil || index == "" {
		return Request{}, errors.New("DSL index is required")
	}
	if err := checkIndex(index, pattern); err != nil {
		return Request{}, err
	}
	body, err := prepareSearch(object["body"], pageSize, page)
	if err != nil {
		return Request{}, err
	}
	return Request{method: http.MethodPost, path: "/" + index + "/_search", index: index, body: body.data, search: true, size: body.size, page: page, windowEnd: body.windowEnd, valid: true}, nil
}

func parseREST(text string, pageSize, page int, pattern string) (Request, error) {
	line, body, _ := strings.Cut(strings.TrimSpace(text), "\n")
	parts := strings.Fields(line)
	if len(parts) != 2 || (parts[0] != http.MethodGet && parts[0] != http.MethodPost) {
		return Request{}, errors.New("REST request requires an allowed method and path")
	}
	endpoint := parts[1]
	if !strings.HasPrefix(endpoint, "/") || strings.ContainsAny(endpoint, "#\\") || strings.Contains(endpoint, "//") {
		return Request{}, errors.New("invalid REST path")
	}
	u, err := url.ParseRequestURI(endpoint)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" {
		return Request{}, errors.New("invalid REST path")
	}
	segments := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	if len(segments) < 2 || len(segments) > 3 {
		return Request{}, errors.New("REST operation is not supported")
	}
	if strings.Contains(segments[0], "%") {
		return Request{}, errors.New("invalid index or alias")
	}
	if err := checkIndex(segments[0], pattern); err != nil {
		return Request{}, err
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Request{}, errors.New("invalid REST parameters")
	}
	r := Request{method: parts[0], path: "/" + segments[0] + "/" + segments[1], index: segments[0], page: page, valid: true}
	if page != 1 && segments[1] != "_search" {
		return Request{}, errors.New("inspection operations support only page one")
	}
	if len(segments) == 3 {
		if segments[1] != "_doc" && segments[1] != "_explain" {
			return Request{}, errors.New("REST operation is not supported")
		}
		id, err := url.PathUnescape(segments[2])
		if err != nil || id == "" || strings.Contains(id, "..") || strings.ContainsAny(id, "\x00\r\n") || strings.Contains(segments[0], "*") {
			return Request{}, errors.New("invalid document ID or index")
		}
		r.path += "/" + url.PathEscape(id)
	}
	allowedParameter := ""
	switch segments[1] {
	case "_doc", "_explain":
		allowedParameter = "routing"
	case "_field_caps":
		allowedParameter = "fields"
	}
	for key, vals := range values {
		if key != allowedParameter || len(vals) != 1 || vals[0] == "" || strings.ContainsAny(vals[0], "\x00\r\n") {
			return Request{}, errors.New("unsupported REST parameter")
		}
	}
	if segments[1] == "_field_caps" {
		fields := values.Get("fields")
		if fields == "" {
			return Request{}, errors.New("field capabilities require fields")
		}
		for _, c := range fields {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("_.*-,", c)) {
				return Request{}, errors.New("invalid field capabilities fields")
			}
		}
	}
	if len(values) != 0 {
		r.path += "?" + values.Encode()
	}
	switch segments[1] {
	case "_search":
		if len(segments) != 2 || parts[0] != http.MethodPost || len(values) != 0 {
			return Request{}, errors.New("search requires POST")
		}
		if strings.TrimSpace(body) == "" {
			body = "{}"
		}
		prepared, err := prepareSearch([]byte(body), pageSize, page)
		if err != nil {
			return Request{}, err
		}
		r.body, r.size, r.search, r.windowEnd = prepared.data, prepared.size, true, prepared.windowEnd
	case "_mapping", "_settings", "_alias", "_stats":
		if len(segments) != 2 || parts[0] != http.MethodGet || strings.TrimSpace(body) != "" || len(values) != 0 {
			return Request{}, errors.New("metadata operation requires GET without a body")
		}
	case "_field_caps":
		if len(segments) != 2 || parts[0] != http.MethodGet || strings.TrimSpace(body) != "" {
			return Request{}, errors.New("field capabilities require GET without a body")
		}
		r.kind = "field_caps"
	case "_doc":
		if len(segments) != 3 || parts[0] != http.MethodGet || strings.TrimSpace(body) != "" {
			return Request{}, errors.New("document lookup requires GET without a body")
		}
		r.kind = "document"
	case "_count", "_explain":
		if (segments[1] == "_count" && len(segments) != 2) || (segments[1] == "_explain" && len(segments) != 3) {
			return Request{}, errors.New("invalid inspection path")
		}
		if segments[1] == "_explain" && parts[0] != http.MethodPost {
			return Request{}, errors.New("explain requires POST")
		}
		if parts[0] == http.MethodGet && strings.TrimSpace(body) != "" {
			return Request{}, errors.New("count GET must not include a body")
		}
		if parts[0] == http.MethodPost {
			object, err := jsonObject([]byte(body))
			if err != nil || len(object) > 1 || containsScript(object) {
				return Request{}, errors.New("inspection body must be a non-scripted query object")
			}
			if len(object) == 1 {
				if _, ok := object["query"]; !ok {
					return Request{}, errors.New("inspection body supports only query")
				}
			}
			if segments[1] == "_explain" && object["query"] == nil {
				return Request{}, errors.New("explain requires a query")
			}
			r.body = []byte(body)
		}
		r.kind = strings.TrimPrefix(segments[1], "_")
	default:
		return Request{}, errors.New("REST operation is not supported")
	}
	return r, nil
}

func checkIndex(index, pattern string) error {
	if index == "" || index == "." || strings.Contains(index, "..") || strings.ContainsAny(index, "/\\%?#[] ,") {
		return errors.New("invalid index or alias")
	}
	for _, c := range index {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '*') {
			return errors.New("invalid index or alias")
		}
	}
	if pattern == "" {
		return errors.New("index pattern is required")
	}
	for _, allowed := range strings.Split(pattern, ",") {
		if index == allowed {
			return nil
		}
		if strings.ContainsAny(index, "*?") {
			continue
		}
		matched, err := path.Match(allowed, index)
		if err != nil {
			return errors.New("invalid configured index pattern")
		}
		if matched {
			return nil
		}
	}
	return errors.New("index is outside the configured index pattern")
}

type preparedBody struct {
	data      []byte
	size      int
	windowEnd bool
}

func prepareSearch(data []byte, pageSize, page int) (preparedBody, error) {
	object, err := jsonObject(data)
	if err != nil {
		return preparedBody{}, err
	}
	allowed := map[string]bool{
		"query": true, "size": true, "sort": true, "_source": true, "fields": true,
		"aggregations": true, "aggs": true, "track_total_hits": true, "min_score": true,
		"post_filter": true, "timeout": true, "highlight": true,
		"explain": true, "track_scores": true, "version": true, "seq_no_primary_term": true,
		"stored_fields": true, "docvalue_fields": true,
	}
	for key := range object {
		if !allowed[key] {
			return preparedBody{}, fmt.Errorf("search option %q is not supported", key)
		}
	}
	for _, key := range []string{"from", "search_after", "pit", "slice", "scroll", "ext", "script_fields", "runtime_mappings"} {
		if _, exists := object[key]; exists {
			return preparedBody{}, fmt.Errorf("search option %q is not supported", key)
		}
	}
	if containsScript(object) {
		return preparedBody{}, errors.New("scripted search is not supported")
	}
	size := pageSize
	if raw, exists := object["size"]; exists {
		var number json.Number
		if err := json.Unmarshal(raw, &number); err != nil {
			return preparedBody{}, errors.New("search size must be an integer")
		}
		parsed, err := strconv.Atoi(number.String())
		if err != nil || parsed < 0 || parsed > 1000 {
			return preparedBody{}, errors.New("search size must be between 0 and 1000")
		}
		size = parsed
	}
	requested := size
	if size == 0 && page != 1 {
		return preparedBody{}, errors.New("aggregation-only search supports only page one")
	}
	from := 0
	if size > 0 {
		if page > 10000/size+1 {
			return preparedBody{}, errors.New("page exceeds the bounded result window")
		}
		from = (page - 1) * size
		if from >= 10000 {
			return preparedBody{}, errors.New("page exceeds the bounded result window")
		}
		requested++
	}
	windowEnd := from+requested > 10000
	if windowEnd {
		requested = 10000 - from
	}
	if from != 0 {
		object["from"] = json.RawMessage(strconv.Itoa(from))
	}
	object["size"] = json.RawMessage(strconv.Itoa(requested))
	encoded, err := json.Marshal(object)
	if err != nil {
		return preparedBody{}, err
	}
	return preparedBody{data: encoded, size: size, windowEnd: windowEnd}, nil
}

func jsonObject(data []byte) (map[string]json.RawMessage, error) {
	if !json.Valid(data) {
		return nil, errors.New("request body must be valid JSON")
	}
	var unique map[string]any
	if err := yaml.Unmarshal(data, &unique); err != nil {
		return nil, errors.New("request body must be a JSON object with unique keys")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, errors.New("request body must be a JSON object")
	}
	return object, nil
}

func containsScript(object map[string]json.RawMessage) bool {
	for key, raw := range object {
		if key == "script" || key == "script_score" || key == "scripted_metric" {
			return true
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&value) == nil && containsScriptValue(value) {
			return true
		}
	}
	return false
}

func containsScriptValue(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, nested := range value {
			if key == "script" || key == "script_score" || key == "scripted_metric" || containsScriptValue(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range value {
			if containsScriptValue(nested) {
				return true
			}
		}
	}
	return false
}

type Pagination struct {
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
	TotalRows *int64 `json:"total_rows"`
	HasMore   bool   `json:"has_more"`
}

type Result struct {
	Columns           []string            `json:"columns"`
	Rows              [][]json.RawMessage `json:"rows"`
	AffectedRows      int                 `json:"affected_rows"`
	Truncated         bool                `json:"truncated"`
	Pagination        *Pagination         `json:"pagination"`
	AdditionalResults []Result            `json:"additional_results,omitempty"`
}

func Normalize(request Request, raw []byte) (Result, error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	object, err := jsonObject(raw)
	if err != nil {
		return Result{}, errors.New("invalid OpenSearch response")
	}
	if !request.search {
		if request.kind == "count" {
			var count struct {
				Count  *int64 `json:"count"`
				Shards *struct {
					Failed *int `json:"failed"`
				} `json:"_shards"`
			}
			if err := json.Unmarshal(raw, &count); err != nil || count.Count == nil || *count.Count < 0 || count.Shards == nil || count.Shards.Failed == nil || *count.Shards.Failed != 0 {
				return Result{}, errors.New("count returned incomplete results")
			}
		}
		return structured(raw), nil
	}
	var completeness struct {
		TimedOut *bool `json:"timed_out"`
		Shards   *struct {
			Failed *int `json:"failed"`
		} `json:"_shards"`
		Hits *struct {
			Hits *[]json.RawMessage `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &completeness); err != nil || completeness.TimedOut == nil || completeness.Shards == nil || completeness.Shards.Failed == nil || *completeness.Shards.Failed < 0 || (request.size > 0 && (completeness.Hits == nil || completeness.Hits.Hits == nil)) {
		return Result{}, errors.New("search response lacks completeness information")
	}
	var result struct {
		TimedOut        bool `json:"timed_out"`
		TerminatedEarly bool `json:"terminated_early"`
		Shards          struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
		Hits struct {
			Total struct {
				Value    int64  `json:"value"`
				Relation string `json:"relation"`
			} `json:"total"`
			Hits []struct {
				Index  json.RawMessage `json:"_index"`
				ID     json.RawMessage `json:"_id"`
				Score  json.RawMessage `json:"_score"`
				Source json.RawMessage `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return Result{}, errors.New("invalid search response")
	}
	if result.TimedOut || result.TerminatedEarly || result.Shards.Failed != 0 {
		return Result{}, errors.New("search returned incomplete results")
	}
	if request.size == 0 {
		return structured(raw), nil
	}
	if _, ok := object["hits"]; !ok {
		return Result{}, errors.New("search response does not contain hits")
	}
	more := len(result.Hits.Hits) > request.size
	if more {
		result.Hits.Hits = result.Hits.Hits[:request.size]
	}
	fields := map[string]bool{}
	for _, hit := range result.Hits.Hits {
		var source map[string]json.RawMessage
		if json.Unmarshal(hit.Source, &source) == nil {
			for name, value := range source {
				if len(value) > 0 && value[0] != '{' && value[0] != '[' {
					fields[name] = true
				}
			}
		}
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	output := Result{Columns: []string{"_index", "_id", "_score", "_source"}, Rows: make([][]json.RawMessage, 0, len(result.Hits.Hits)), Pagination: &Pagination{Page: request.page, PageSize: request.size, HasMore: more}}
	if request.windowEnd && len(result.Hits.Hits) == 10000-(request.page-1)*request.size {
		output.Truncated = result.Hits.Total.Relation != "eq" || result.Hits.Total.Value > int64((request.page-1)*request.size+len(result.Hits.Hits))
	}
	for _, name := range names {
		output.Columns = append(output.Columns, "_source."+name)
	}
	if result.Hits.Total.Relation == "eq" && result.Hits.Total.Value <= 9007199254740991 {
		output.Pagination.TotalRows = &result.Hits.Total.Value
	}
	for _, hit := range result.Hits.Hits {
		row := []json.RawMessage{nullIfEmpty(hit.Index), nullIfEmpty(hit.ID), LosslessScalar(hit.Score), JSONText(hit.Source)}
		var source map[string]json.RawMessage
		_ = json.Unmarshal(hit.Source, &source)
		for _, name := range names {
			row = append(row, LosslessScalar(source[name]))
		}
		output.Rows = append(output.Rows, row)
	}
	if aggregations, ok := object["aggregations"]; ok {
		output.AdditionalResults = []Result{structured(aggregations)}
	}
	if result.Hits.Total.Relation == "eq" && result.Hits.Total.Value > 9007199254740991 {
		var hits map[string]json.RawMessage
		if err := json.Unmarshal(object["hits"], &hits); err != nil {
			return Result{}, errors.New("invalid OpenSearch hit total")
		}
		total, err := json.Marshal(map[string]json.RawMessage{"total": hits["total"]})
		if err != nil {
			return Result{}, err
		}
		output.AdditionalResults = append(output.AdditionalResults, structured(total))
	}
	return output, nil
}

func nullIfEmpty(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return value
}

func LosslessScalar(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	if value[0] != '-' && (value[0] < '0' || value[0] > '9') {
		return value
	}
	text := string(value)
	if len(text) <= 128 {
		f, err := strconv.ParseFloat(text, 64)
		if err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
			if f == 0 {
				if text == "0" {
					return value
				}
				encoded, err := json.Marshal(text)
				if err != nil {
					panic(err)
				}
				return encoded
			}
			rational, ok := new(big.Rat).SetString(text)
			if ok && rational.Cmp(new(big.Rat).SetFloat64(f)) == 0 {
				return value
			}
		}
	}
	encoded, err := json.Marshal(text)
	if err != nil {
		panic(err)
	}
	return encoded
}

func JSONText(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	encoded, err := json.Marshal(string(value))
	if err != nil {
		panic(err)
	}
	return encoded
}

func structured(value []byte) Result {
	return Result{Columns: []string{"response"}, Rows: [][]json.RawMessage{{JSONText(value)}}}
}

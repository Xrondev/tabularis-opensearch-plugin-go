package browse

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	sqlglot "github.com/ridi-oss/sqlglot-go"
	"github.com/ridi-oss/sqlglot-go/expressions"

	"tabularis-opensearch-plugin-go/internal/opensearch"
	"tabularis-opensearch-plugin-go/internal/query"
	"tabularis-opensearch-plugin-go/internal/sqlquery"
)

type Plan struct {
	Request   query.Request
	count     bool
	countName string
	limit     int
	pageSize  int
	offset    int
	exhausted bool
}

func (p Plan) PITAllowed() bool {
	return !p.count && p.limit != 0
}

func NormalizePIT(plan Plan, raw []byte, page int) (query.Result, error) {
	if page < 1 || page-1 > int(^uint(0)>>1)/plan.pageSize {
		return query.Result{}, errors.New("invalid PIT page")
	}
	plan.offset = (page - 1) * plan.pageSize
	if plan.limit >= 0 && plan.offset >= plan.limit {
		return query.Result{}, errors.New("page exceeds SQL LIMIT")
	}
	result, err := Normalize(plan, raw)
	if err == nil && result.Pagination != nil {
		result.Pagination.Page = page
	}
	return result, err
}

func Candidate(text string) (bool, error) {
	statements, err := sqlglot.Parse(text, "")
	if err != nil || len(statements) != 1 || statements[0] == nil {
		return false, errors.New("invalid browse SQL")
	}
	statement := statements[0]
	if statement.Kind() != expressions.KindSelect || statement.Arg("group") != nil || statement.Arg("having") != nil {
		return false, nil
	}
	selects := statement.Selects()
	if len(selects) != 1 {
		return false, nil
	}
	_, _, candidate := projection(selects[0])
	return candidate, nil
}

func projection(selected expressions.Expression) (bool, string, bool) {
	if selected == nil {
		return false, "", false
	}
	if selected.Kind() == expressions.KindStar {
		return false, "", true
	}
	name := "COUNT(*)"
	if selected.Kind() == expressions.KindAlias {
		name = selected.Alias()
		selected = selected.This()
	}
	if name != "" && selected != nil && selected.Kind() == expressions.KindCount && selected.This() != nil && selected.This().Kind() == expressions.KindStar {
		return true, name, true
	}
	return false, "", false
}

func Translate(text, pattern string, pageSize, page int, columns []opensearch.Column, verifiedExactFields map[string]bool) (Plan, error) {
	if page < 1 {
		return Plan{}, errors.New("page must be at least one")
	}
	validated, err := sqlquery.Validate(text, pattern, pageSize)
	if err != nil {
		return Plan{}, err
	}
	statements, err := sqlglot.Parse(text, "")
	if err != nil || len(statements) != 1 || statements[0] == nil {
		return Plan{}, errors.New("invalid browse SQL")
	}
	statement := statements[0]
	if statement.Kind() != expressions.KindSelect || statement.Arg("group") != nil || statement.Arg("having") != nil {
		return Plan{}, errors.New("browse SQL requires a simple SELECT")
	}
	selects := statement.Selects()
	if len(selects) != 1 {
		return Plan{}, errors.New("browse SQL supports SELECT * or COUNT(*)")
	}
	count, countName, candidate := projection(selects[0])
	if !candidate {
		return Plan{}, errors.New("browse SQL supports SELECT * or COUNT(*)")
	}
	types := make(map[string]string, len(columns))
	for _, column := range columns {
		if _, exists := types[column.Name]; exists || column.Comment != "source field" {
			types[column.Name] = ""
		} else {
			types[column.Name] = column.DataType
		}
	}
	body := map[string]any{"query": map[string]any{"match_all": map[string]any{}}}
	if where, ok := statement.Arg("where").(expressions.Expression); ok && where != nil {
		clause, err := predicate(where.This(), types, verifiedExactFields)
		if err != nil {
			return Plan{}, err
		}
		body["query"] = clause
	}
	if order, ok := statement.Arg("order").(expressions.Expression); ok && order != nil {
		if count {
			return Plan{}, errors.New("COUNT(*) does not support ordering")
		}
		sorts := make([]any, 0, len(order.Expressions()))
		for _, item := range order.Expressions() {
			if item.Kind() != expressions.KindOrdered || item.This() == nil || item.This().Kind() != expressions.KindColumn {
				return Plan{}, errors.New("unsupported browse sort")
			}
			field, err := mappedField(item.This(), types, verifiedExactFields)
			if err != nil {
				return Plan{}, err
			}
			direction := "asc"
			if desc, _ := item.Arg("desc").(bool); desc {
				direction = "desc"
			}
			missing := "_last"
			if first, _ := item.Arg("nulls_first").(bool); first {
				missing = "_first"
			}
			sorts = append(sorts, map[string]any{field: map[string]string{"order": direction, "missing": missing}})
		}
		body["sort"] = sorts
	}
	limit := -1
	if limitNode, ok := statement.Arg("limit").(expressions.Expression); ok && limitNode != nil {
		value := limitNode.Expr()
		if value == nil || !value.IsInt() {
			return Plan{}, errors.New("browse limit must be a nonnegative integer")
		}
		limit, err = strconv.Atoi(value.Name())
		if err != nil || limit < 0 {
			return Plan{}, errors.New("browse limit must be a nonnegative integer")
		}
	}
	offset := 0
	exhausted := false
	requestPage := page
	if count {
		exhausted = page > 1 || limit == 0
		requestPage = 1
		body["size"] = 0
		body["track_total_hits"] = true
	} else {
		if page-1 > 9999/pageSize {
			return Plan{}, errors.New("page exceeds the bounded result window")
		}
		offset = (page - 1) * pageSize
		exhausted = limit >= 0 && offset >= limit
	}
	if exhausted {
		body["query"] = map[string]any{"match_none": map[string]any{}}
	}
	envelope, err := json.Marshal(map[string]any{"index": validated.Index(), "body": body})
	if err != nil {
		return Plan{}, err
	}
	request, err := query.Parse("#!dsl\n"+string(envelope), pageSize, requestPage, pattern)
	if err != nil {
		return Plan{}, err
	}
	return Plan{Request: request, count: count, countName: countName, limit: limit, pageSize: pageSize, offset: offset, exhausted: exhausted}, nil
}

func Normalize(plan Plan, raw []byte) (query.Result, error) {
	if plan.count {
		var result struct {
			TimedOut bool `json:"timed_out"`
			Shards   struct {
				Failed int `json:"failed"`
			} `json:"_shards"`
			Hits struct {
				Total struct {
					Value    json.Number `json:"value"`
					Relation string      `json:"relation"`
				} `json:"total"`
			} `json:"hits"`
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &result); err != nil || json.Unmarshal(raw, &fields) != nil || fields["timed_out"] == nil || fields["_shards"] == nil || result.TimedOut || result.Shards.Failed != 0 || result.Hits.Total.Relation != "eq" {
			return query.Result{}, errors.New("count response is invalid or incomplete")
		}
		value := result.Hits.Total.Value.String()
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return query.Result{}, errors.New("count response is invalid")
		}
		rows := [][]json.RawMessage{}
		if !plan.exhausted {
			rows = append(rows, []json.RawMessage{query.LosslessScalar(json.RawMessage(value))})
		}
		return query.Result{Columns: []string{plan.countName}, Rows: rows}, nil
	}
	result, err := query.Normalize(plan.Request, raw)
	if err != nil {
		return query.Result{}, err
	}
	if plan.limit >= 0 && result.Pagination != nil {
		if plan.exhausted {
			result.Pagination.TotalRows = nil
			if plan.limit == 0 {
				zero := int64(0)
				result.Pagination.TotalRows = &zero
			}
		} else {
			remaining := plan.limit - plan.offset
			if len(result.Rows) > remaining {
				result.Rows = result.Rows[:remaining]
			}
			if result.Pagination.TotalRows != nil && *result.Pagination.TotalRows > int64(plan.limit) {
				limited := int64(plan.limit)
				result.Pagination.TotalRows = &limited
			}
		}
		if plan.offset+len(result.Rows) >= plan.limit {
			result.Pagination.HasMore = false
			result.Truncated = false
		}
	}
	return result, nil
}

func mappedField(column expressions.Expression, types map[string]string, verifiedExactFields map[string]bool) (string, error) {
	if column.TableName() != "" || column.SchemaName() != "" || column.CatalogName() != "" {
		return "", errors.New("qualified browse fields are not supported")
	}
	name := column.Name()
	if strings.HasPrefix(name, "_source.") {
		name = strings.TrimPrefix(name, "_source.")
	}
	if name == "" || strings.Contains(name, ".") {
		return "", errors.New("dotted, nested, or multi-field browse columns are ambiguous")
	}
	if !verifiedExactFields[name] {
		return "", fmt.Errorf("field %q lacks verified scalar and null semantics", name)
	}
	typ := types[name]
	switch typ {
	case "byte", "short", "integer", "long", "unsigned_long", "half_float", "float", "double", "scaled_float", "keyword", "boolean":
		return name, nil
	default:
		return "", fmt.Errorf("field %q is missing or does not support exact browse operations", name)
	}
}

func predicate(node expressions.Expression, types map[string]string, verifiedExactFields map[string]bool) (map[string]any, error) {
	if node == nil {
		return nil, errors.New("invalid browse filter")
	}
	if node.Kind() == expressions.KindParen {
		return predicate(node.This(), types, verifiedExactFields)
	}
	if node.Kind() == expressions.KindAnd || node.Kind() == expressions.KindOr {
		left, err := predicate(node.This(), types, verifiedExactFields)
		if err != nil {
			return nil, err
		}
		right, err := predicate(node.Expr(), types, verifiedExactFields)
		if err != nil {
			return nil, err
		}
		if node.Kind() == expressions.KindAnd {
			return map[string]any{"bool": map[string]any{"filter": []any{left, right}}}, nil
		}
		return map[string]any{"bool": map[string]any{"should": []any{left, right}, "minimum_should_match": 1}}, nil
	}
	column := node.This()
	if column == nil || column.Kind() != expressions.KindColumn {
		return nil, errors.New("browse filter requires a mapped field on the left")
	}
	field, err := mappedField(column, types, verifiedExactFields)
	if err != nil {
		return nil, err
	}
	if node.Kind() == expressions.KindIn {
		items := make([]any, 0, len(node.Expressions()))
		for _, item := range node.Expressions() {
			value, err := scalar(item, types[field])
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		if len(items) == 0 {
			return nil, errors.New("browse IN requires values")
		}
		return map[string]any{"terms": map[string]any{field: items}}, nil
	}
	value, err := scalar(node.Expr(), types[field])
	if err != nil {
		return nil, err
	}
	switch node.Kind() {
	case expressions.KindEQ:
		return map[string]any{"term": map[string]any{field: value}}, nil
	case expressions.KindNEQ:
		return map[string]any{"bool": map[string]any{"filter": []any{map[string]any{"exists": map[string]any{"field": field}}}, "must_not": []any{map[string]any{"term": map[string]any{field: value}}}}}, nil
	case expressions.KindGT, expressions.KindGTE, expressions.KindLT, expressions.KindLTE:
		operator := map[expressions.Kind]string{expressions.KindGT: "gt", expressions.KindGTE: "gte", expressions.KindLT: "lt", expressions.KindLTE: "lte"}[node.Kind()]
		return map[string]any{"range": map[string]any{field: map[string]any{operator: value}}}, nil
	default:
		return nil, errors.New("unsupported browse filter")
	}
}

func scalar(node expressions.Expression, typ string) (any, error) {
	if node == nil {
		return nil, errors.New("browse filter requires a scalar literal")
	}
	if typ == "keyword" && node.IsString() {
		return node.Name(), nil
	}
	if typ == "boolean" && node.Kind() == expressions.KindBoolean {
		value, ok := node.Arg("this").(bool)
		if ok {
			return value, nil
		}
	}
	if typ != "keyword" && typ != "boolean" && node.IsNumber() {
		value := node.Name()
		if strings.TrimSpace(value) == "" || !json.Valid([]byte(value)) {
			return nil, errors.New("invalid numeric literal")
		}
		return json.Number(value), nil
	}
	return nil, errors.New("filter literal does not match field type")
}

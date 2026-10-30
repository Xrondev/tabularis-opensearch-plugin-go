package sqlquery

import (
	"encoding/json"
	"errors"
	"path"
	"strconv"
	"strings"

	sqlglot "github.com/ridi-oss/sqlglot-go"
	"github.com/ridi-oss/sqlglot-go/expressions"
	"github.com/ridi-oss/sqlglot-go/generator"

	"tabularis-opensearch-plugin-go/internal/query"
)

type Query struct {
	sql           string
	index         string
	size          int
	page          int
	ordered       bool
	windowLimited bool
}

func (q Query) SQL() string   { return q.sql }
func (q Query) Index() string { return q.index }
func (q Query) Validate() error {
	if q.sql == "" || q.index == "" || q.size < 1 || q.size > 1000 || q.page < 1 {
		return errors.New("invalid SQL query")
	}
	return nil
}

func Validate(text, indexPattern string, pageSize int) (Query, error) {
	return ValidatePage(text, indexPattern, pageSize, 1)
}

func ValidatePage(text, indexPattern string, pageSize, page int) (Query, error) {
	const maxWindow = 10000
	if pageSize < 1 || pageSize > 1000 || page < 1 || page-1 > (maxWindow-1)/pageSize || indexPattern == "" {
		return Query{}, errors.New("invalid SQL page size or index scope")
	}
	offset := (page - 1) * pageSize
	statements, err := sqlglot.Parse(text, "")
	if err != nil || len(statements) != 1 || statements[0] == nil || statements[0].Kind() != expressions.KindSelect {
		return Query{}, errors.New("SQL must contain one supported read statement")
	}
	statement := statements[0]
	order, ordered := statement.Arg("order").(expressions.Expression)
	ordered = ordered && order != nil
	if page > 1 && !ordered {
		return Query{}, errors.New("SQL page navigation requires ORDER BY")
	}
	index := ""
	tables := 0
	selects := 0
	for _, node := range statement.Walk() {
		if len(node.Comments()) != 0 {
			return Query{}, errors.New("SQL comments are not supported")
		}
		if node.Kind() == expressions.KindLiteral && node.IsString() && strings.ContainsAny(node.Name(), "'\\") {
			return Query{}, errors.New("SQL string escape forms are not supported")
		}
		switch node.Kind() {
		case expressions.KindSelect:
			selects++
		case expressions.KindTable:
			tables++
			if node.SchemaName() != "" || node.CatalogName() != "" || !withinScope(node.Name(), indexPattern) {
				return Query{}, errors.New("SQL index is outside the configured scope")
			}
			index = node.Name()
		case expressions.KindColumn, expressions.KindLiteral, expressions.KindIdentifier,
			expressions.KindStar, expressions.KindAlias, expressions.KindNull, expressions.KindBoolean,
			expressions.KindParen, expressions.KindEQ, expressions.KindNEQ, expressions.KindGT,
			expressions.KindGTE, expressions.KindLT, expressions.KindLTE, expressions.KindAnd,
			expressions.KindOr, expressions.KindIn, expressions.KindIs, expressions.KindFrom,
			expressions.KindWhere, expressions.KindGroup, expressions.KindOrder, expressions.KindLimit,
			expressions.KindHaving, expressions.KindOrdered, expressions.KindCount, expressions.KindAvg,
			expressions.KindSum, expressions.KindMin, expressions.KindMax:
		default:
			return Query{}, errors.New("SQL contains an unsupported expression")
		}
	}
	if selects != 1 || tables != 1 {
		return Query{}, errors.New("SQL requires exactly one index")
	}
	limit := pageSize + 1
	windowLimited := false
	if limit > maxWindow-offset {
		limit = maxWindow - offset
		windowLimited = true
	}
	if old, ok := statement.Arg("limit").(expressions.Expression); ok && old != nil {
		value := old.Expr()
		if value == nil || !value.IsInt() {
			return Query{}, errors.New("SQL limit must be an integer")
		}
		count, err := strconv.Atoi(value.Name())
		if err != nil || count < 0 {
			return Query{}, errors.New("SQL limit must be an integer")
		}
		remaining := count - offset
		if remaining < 0 {
			remaining = 0
		}
		if remaining <= limit {
			limit = remaining
			windowLimited = false
		}
	}
	statement.Set("limit", expressions.Limit(expressions.Args{"expression": expressions.LiteralNumber(limit)}))
	if offset > 0 {
		statement.Set("offset", expressions.Offset(expressions.Args{"expression": expressions.LiteralNumber(offset)}))
	}
	rendered, err := sqlglot.Generate(statement, "mysql", generator.Options{})
	if err != nil {
		return Query{}, errors.New("unable to render validated SQL")
	}
	return Query{sql: rendered, index: index, size: pageSize, page: page, ordered: ordered, windowLimited: windowLimited}, nil
}

func Normalize(request Query, raw []byte) (query.Result, error) {
	if err := request.Validate(); err != nil {
		return query.Result{}, err
	}
	var data struct {
		Schema []struct {
			Name string `json:"name"`
		} `json:"schema"`
		Rows   [][]json.RawMessage `json:"datarows"`
		Cursor string              `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &data); err != nil || data.Schema == nil || data.Rows == nil {
		return query.Result{}, errors.New("invalid SQL response")
	}
	if data.Cursor != "" {
		return query.Result{}, errors.New("SQL cursor pagination is not supported")
	}
	result := query.Result{
		Columns:    make([]string, 0, len(data.Schema)),
		Rows:       make([][]json.RawMessage, 0, len(data.Rows)),
		Pagination: &query.Pagination{Page: request.page, PageSize: request.size},
	}
	for _, column := range data.Schema {
		if column.Name == "" {
			return query.Result{}, errors.New("invalid SQL result column")
		}
		result.Columns = append(result.Columns, column.Name)
	}
	for _, row := range data.Rows {
		if len(row) != len(data.Schema) {
			return query.Result{}, errors.New("SQL row does not match its schema")
		}
		for index, value := range row {
			if len(value) != 0 && (value[0] == '{' || value[0] == '[') {
				row[index] = query.JSONText(value)
			} else {
				row[index] = query.LosslessScalar(value)
			}
		}
	}
	if len(data.Rows) > request.size {
		result.Pagination.HasMore = request.ordered
		result.Truncated = !request.ordered
		data.Rows = data.Rows[:request.size]
	}
	if request.windowLimited && len(data.Rows) == request.size {
		result.Truncated = true
	}
	result.Rows = append(result.Rows, data.Rows...)
	return result, nil
}

func withinScope(index, pattern string) bool {
	if index == "" || strings.ContainsAny(index, "/\\%?#[] ,") || strings.Contains(index, "..") {
		return false
	}
	for _, allowed := range strings.Split(pattern, ",") {
		if index == allowed {
			return true
		}
		if strings.Contains(index, "*") {
			continue
		}
		matched, err := path.Match(allowed, index)
		if err == nil && matched {
			return true
		}
	}
	return false
}

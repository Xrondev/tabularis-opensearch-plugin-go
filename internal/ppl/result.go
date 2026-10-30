package ppl

import (
	"encoding/json"
	"errors"

	"tabularis-opensearch-plugin-go/internal/query"
)

func Normalize(request Query, raw []byte) (query.Result, error) {
	if err := request.Validate(); err != nil {
		return query.Result{}, err
	}
	var response struct {
		Schema []struct {
			Name string `json:"name"`
		} `json:"schema"`
		Rows   [][]json.RawMessage `json:"datarows"`
		Cursor string              `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Schema == nil || response.Rows == nil {
		return query.Result{}, errors.New("invalid PPL response")
	}
	if response.Cursor != "" {
		return query.Result{}, errors.New("PPL cursor responses are not supported")
	}
	result := query.Result{Columns: make([]string, 0, len(response.Schema)), Rows: make([][]json.RawMessage, 0, len(response.Rows))}
	for _, column := range response.Schema {
		if column.Name == "" {
			return query.Result{}, errors.New("invalid PPL result column")
		}
		result.Columns = append(result.Columns, column.Name)
	}
	for _, row := range response.Rows {
		if len(row) != len(response.Schema) {
			return query.Result{}, errors.New("PPL row does not match its schema")
		}
		for index, value := range row {
			if len(value) != 0 && (value[0] == '{' || value[0] == '[') {
				row[index] = query.JSONText(value)
			} else {
				row[index] = query.LosslessScalar(value)
			}
		}
	}
	if len(response.Rows) > request.size {
		response.Rows = response.Rows[:request.size]
		result.Truncated = true
	}
	result.Rows = append(result.Rows, response.Rows...)
	return result, nil
}

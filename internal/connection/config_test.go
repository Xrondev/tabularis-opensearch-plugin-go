package connection_test

import (
	"testing"

	"tabularis-opensearch-plugin-go/internal/connection"
)

func TestPaginationMode(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
		valid bool
	}{
		{"", "offset", true},
		{"offset", "offset", true},
		{"pit", "pit", true},
		{"PIT", "", false},
		{"other", "", false},
	} {
		params := connection.Params{Extra: map[string]string{
			"endpoint": "http://localhost:9200", "deployment": "self_hosted", "auth_mode": "anonymous", "pagination_mode": test.input,
		}}
		config, err := connection.Parse(params)
		if (err == nil) != test.valid || (err == nil && config.PaginationMode != test.want) {
			t.Errorf("mode %q: mode=%q err=%v", test.input, config.PaginationMode, err)
		}
	}
}

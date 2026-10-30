package browse_test

import (
	"testing"

	"tabularis-opensearch-plugin-go/internal/browse"
	"tabularis-opensearch-plugin-go/internal/opensearch"
)

func TestBrowseCandidate(t *testing.T) {
	cases := []struct {
		text      string
		candidate bool
	}{
		{`SELECT * FROM "events"`, true},
		{`SELECT * FROM "events" WHERE price > 4`, true},
		{`SELECT COUNT(*) FROM "events"`, true},
		{`SELECT COUNT(*) AS count FROM "events"`, true},
		{`SELECT COUNT(*) FROM "events" GROUP BY category`, false},
		{`SELECT title FROM "events"`, false},
		{`SELECT SUM(price) FROM "events"`, false},
	}
	for _, test := range cases {
		candidate, err := browse.Candidate(test.text)
		if err != nil || candidate != test.candidate {
			t.Errorf("%s: candidate=%v, err=%v", test.text, candidate, err)
		}
	}
	if _, err := browse.Candidate(`SELECT * FROM`); err == nil {
		t.Fatal("invalid SQL was accepted")
	}
}

func TestGeneratedColumnsRequireUnambiguousVerifiedMappings(t *testing.T) {
	statement := `SELECT * FROM "events" WHERE "_source.count" = 3`
	verified := map[string]bool{"count": true}
	column := opensearch.Column{Name: "count", DataType: "long", Comment: "source field"}
	if _, err := browse.Translate(statement, "events", 100, 1, []opensearch.Column{column}, verified); err != nil {
		t.Fatalf("verified top-level field was rejected: %v", err)
	}
	if _, err := browse.Translate(statement, "events", 100, 1, []opensearch.Column{column}, nil); err == nil {
		t.Fatal("mapping alone was treated as verified field semantics")
	}
	if _, err := browse.Translate(statement, "events", 100, 1, []opensearch.Column{column, column}, verified); err == nil {
		t.Fatal("duplicate field mapping was accepted")
	}
	column.Comment = "multi-field"
	if _, err := browse.Translate(statement, "events", 100, 1, []opensearch.Column{column}, verified); err == nil {
		t.Fatal("multi-field mapping was accepted as a top-level source field")
	}
	dotted := `SELECT * FROM "events" WHERE "_source.parent.child" = 3`
	if _, err := browse.Translate(dotted, "events", 100, 1, []opensearch.Column{{Name: "parent.child", DataType: "long", Comment: "source field"}}, map[string]bool{"parent.child": true}); err == nil {
		t.Fatal("dotted field mapping was accepted")
	}
}

func TestCountPreservesLargeIntegers(t *testing.T) {
	plan, err := browse.Translate(`SELECT COUNT(*) AS count FROM "events"`, "events", 100, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := browse.Normalize(plan, []byte(`{"timed_out":false,"_shards":{"failed":0},"hits":{"total":{"value":9007199254740993,"relation":"eq"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "count" || len(result.Rows) != 1 || string(result.Rows[0][0]) != `"9007199254740993"` {
		t.Fatalf("unexpected count result: %+v", result)
	}
}

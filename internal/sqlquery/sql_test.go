package sqlquery_test

import (
	"strings"
	"testing"

	"tabularis-opensearch-plugin-go/internal/sqlquery"
)

func TestReadOnlySQLSubset(t *testing.T) {
	for _, text := range []string{
		`SELECT * FROM "tabularis_fixture"`,
		`SELECT message FROM tabularis_fixture WHERE count >= 2 ORDER BY message`,
		`SELECT COUNT(*) FROM tabularis_fixture`,
		`SELECT message, COUNT(*) FROM tabularis_fixture GROUP BY message`,
	} {
		query, err := sqlquery.Validate(text, "tabularis_*", 100)
		if err != nil {
			t.Errorf("rejected %s: %v", text, err)
			continue
		}
		if query.Index() != "tabularis_fixture" || !strings.Contains(query.SQL(), "LIMIT 101") {
			t.Errorf("unexpected query: %+v", query)
		}
	}
}

func TestRejectUnsafeSQL(t *testing.T) {
	for _, text := range []string{
		`DELETE FROM tabularis_fixture`,
		`SELECT * FROM tabularis_fixture; DELETE FROM tabularis_fixture`,
		`SELECT * FROM tabularis_fixture INTO OUTFILE '/etc/passwd'`,
		`SELECT * FROM tabularis_fixture FOR UPDATE`,
		`SELECT * FROM secret`,
		`SELECT evil(message) FROM tabularis_fixture`,
		`SELECT * FROM tabularis_fixture WHERE count IN (SELECT count FROM secret)`,
		`SELECT * FROM tabularis_fixture LIMIT 1 OFFSET 1`,
		`SELECT * FROM tabularis_fixture WHERE message = 'it''s unsafe'`,
	} {
		if _, err := sqlquery.Validate(text, "tabularis_*", 100); err == nil {
			t.Errorf("accepted %s", text)
		}
	}
}

func TestInvalidSQLResponse(t *testing.T) {
	if _, err := sqlquery.Normalize(sqlquery.Query{}, []byte(`{}`)); err == nil {
		t.Fatal("accepted uninitialized SQL query")
	}
	request, err := sqlquery.Validate("SELECT message FROM tabularis_fixture", "tabularis_*", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, response := range []string{
		`{}`,
		`{"schema":[{"name":"message"}],"datarows":[[]]}`,
		`{"schema":[{"name":"message"}],"datarows":[],"cursor":"opaque"}`,
	} {
		if _, err := sqlquery.Normalize(request, []byte(response)); err == nil {
			t.Errorf("accepted invalid response: %s", response)
		}
	}
}

func TestSQLNumericPrecision(t *testing.T) {
	request, err := sqlquery.Validate("SELECT count FROM tabularis_fixture", "tabularis_*", 1)
	if err != nil {
		t.Fatal(err)
	}
	result, err := sqlquery.Normalize(request, []byte(`{"schema":[{"name":"count"}],"datarows":[[9007199254740993]]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(result.Rows[0][0]); got != `"9007199254740993"` {
		t.Fatalf("large integer was not preserved: %s", got)
	}
}

func TestNumberedSQLPagesPreserveExplicitLimit(t *testing.T) {
	text := `SELECT message FROM tabularis_fixture ORDER BY message LIMIT 3`
	for _, test := range []struct {
		page int
		want string
	}{
		{1, "LIMIT 3"},
		{2, "LIMIT 1 OFFSET 2"},
		{3, "LIMIT 0 OFFSET 4"},
	} {
		request, err := sqlquery.ValidatePage(text, "tabularis_*", 2, test.page)
		if err != nil {
			t.Fatalf("page %d: %v", test.page, err)
		}
		if !strings.Contains(request.SQL(), test.want) {
			t.Errorf("page %d SQL: %s", test.page, request.SQL())
		}
	}
	for _, test := range []struct {
		text string
		page int
	}{
		{`SELECT * FROM tabularis_fixture`, 2},
		{text, 0},
		{text, 10002},
	} {
		if _, err := sqlquery.ValidatePage(test.text, "tabularis_*", 2, test.page); err == nil {
			t.Errorf("accepted unsupported page %d: %s", test.page, test.text)
		}
	}
}

func TestSQLPageMetadata(t *testing.T) {
	ordered, err := sqlquery.ValidatePage(`SELECT message FROM tabularis_fixture ORDER BY message`, "tabularis_*", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	result, err := sqlquery.Normalize(ordered, []byte(`{"schema":[{"name":"message"}],"datarows":[["one"],["two"]]}`))
	if err != nil || result.Pagination.Page != 2 || !result.Pagination.HasMore || result.Truncated || len(result.Rows) != 1 {
		t.Fatalf("unexpected ordered page: %+v; %v", result, err)
	}
	unordered, err := sqlquery.ValidatePage(`SELECT message FROM tabularis_fixture`, "tabularis_*", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	result, err = sqlquery.Normalize(unordered, []byte(`{"schema":[{"name":"message"}],"datarows":[["one"],["two"]]}`))
	if err != nil || result.Pagination.HasMore || !result.Truncated {
		t.Fatalf("unexpected unordered page: %+v; %v", result, err)
	}
	last, err := sqlquery.ValidatePage(`SELECT message FROM tabularis_fixture ORDER BY message`, "tabularis_*", 2, 5000)
	if err != nil {
		t.Fatal(err)
	}
	result, err = sqlquery.Normalize(last, []byte(`{"schema":[{"name":"message"}],"datarows":[["one"],["two"]]}`))
	if err != nil || result.Pagination.HasMore || !result.Truncated {
		t.Fatalf("unexpected bounded-window result: %+v; %v", result, err)
	}
}

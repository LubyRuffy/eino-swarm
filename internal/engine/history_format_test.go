package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/history"
)

func decodeHistoryFormat(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHistoryObjectRowsPreserveExactValuesAndColumnOrder(t *testing.T) {
	identity := historyContext{ThreadID: "current", ProjectID: "project", ProjectName: "项目"}
	const original = "before\x00after<&>界"
	blob := []byte{0, 1, 255}
	source := history.Result{Columns: []string{"seq", "id", "body", "blob", "empty", "absent"}, Rows: [][]any{{int64(3), int64(9007199254740993), original, blob, "", nil}}}
	body, err := formatHistoryResult(identity, source, 10000)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeHistoryFormat(t, body)
	row := got["rows"].([]any)[0].(map[string]any)
	if row["id"].(json.Number).String() != "9007199254740993" || row["seq"].(json.Number).String() != "3" || row["body"] != original || row["blob"] != base64.StdEncoding.EncodeToString(blob) || row["empty"] != "" || row["absent"] != nil {
		t.Fatalf("original SQL values changed: %#v", row)
	}
	if got["columns"].([]any)[0] != "seq" || got["context"].(map[string]any)["project_name"] != identity.ProjectName || got["ok"] != true {
		t.Fatalf("scope/column order changed: %#v", got)
	}
	if source.Rows[0][2] != original || !reflect.DeepEqual(source.Rows[0][3], blob) {
		t.Fatal("formatting mutated the raw reader result")
	}
	if _, ok := got["field_semantics"]; ok {
		t.Fatal("SQL result labels were treated as canonical source fields")
	}
}

func TestHistoryObjectRowsRejectDuplicateLabelsInsteadOfOverwriting(t *testing.T) {
	for _, source := range []history.Result{
		{Columns: []string{"id", "id"}, Rows: [][]any{{1, 2}}},
		{Columns: []string{"id"}, Rows: [][]any{{1, 2}}},
		{Columns: []string{"value"}, Rows: [][]any{{math.Inf(1)}}},
	} {
		if _, err := formatHistoryResult(historyContext{}, source, 10000); err == nil {
			t.Fatalf("invalid raw result was silently rewritten: %#v", source)
		}
	}
	body, err := formatHistoryResult(historyContext{}, history.Result{Columns: []string{"", "ID", "id:1"}, Rows: [][]any{{1, 2, 3}}}, 10000)
	if err != nil || !strings.Contains(body, `"":1`) || !strings.Contains(body, `"ID":2`) || !strings.Contains(body, `"id:1":3`) {
		t.Fatalf("distinct labels changed: %s %v", body, err)
	}
}

func TestHistoryObjectFormattingBudgetsCompleteRowsAndNewTruncationMetadata(t *testing.T) {
	source := history.Result{Columns: []string{"source", "position"}, Rows: [][]any{}}
	for i := range 12 {
		source.Rows = append(source.Rows, []any{strings.Repeat("原始记录<&>\x00", 12), i})
	}
	full, err := formatHistoryResult(historyContext{ProjectName: "项目"}, source, 100000)
	if err != nil {
		t.Fatal(err)
	}
	fullSize := utf8.RuneCountInString(full)
	exact, err := formatHistoryResult(historyContext{ProjectName: "项目"}, source, fullSize)
	if err != nil || exact != full {
		t.Fatalf("exact complete-result budget changed output: %v", err)
	}
	limit := fullSize / 2
	body, err := formatHistoryResult(historyContext{ProjectName: "项目"}, source, limit)
	if err != nil || utf8.RuneCountInString(body) > limit {
		t.Fatalf("complete result exceeds cap: chars=%d cap=%d err=%v", utf8.RuneCountInString(body), limit, err)
	}
	got := decodeHistoryFormat(t, body)
	rows := got["rows"].([]any)
	if got["truncated"] != true || got["reason"] != historyFormatBudgetReason || len(rows) == 0 || len(rows) >= len(source.Rows) || got["row_count"].(json.Number).String() != stringInt(len(rows)) {
		t.Fatalf("missing whole-row truncation: %#v", got)
	}
	for i, value := range rows {
		row := value.(map[string]any)
		if row["source"] != source.Rows[i][0] || row["position"].(json.Number).String() != stringInt(i) {
			t.Fatalf("source row was clipped or reordered: %#v", row)
		}
	}
	if len(source.Rows) != 12 {
		t.Fatal("budgeting removed original reader rows")
	}
	// A notice can itself require removing rows which fit before truncation.
	for _, maxChars := range []int{fullSize - 1, fullSize - 2} {
		body, err := formatHistoryResult(historyContext{ProjectName: "项目"}, source, maxChars)
		if err != nil || utf8.RuneCountInString(body) > maxChars || decodeHistoryFormat(t, body)["truncated"] != true {
			t.Fatalf("truncation notice exceeded its budget: %s %v", body, err)
		}
	}
	source.Truncated, source.Reason = true, "original SQL result budget was reached"
	body, err = formatHistoryResult(historyContext{}, source, 100000)
	if err != nil || decodeHistoryFormat(t, body)["reason"] != source.Reason {
		t.Fatalf("reader truncation was lost: %s %v", body, err)
	}
}

func stringInt(n int) string {
	body, _ := json.Marshal(n)
	return string(body)
}

func TestHistoryFormattingRefusesWhenRequiredMetadataCannotFit(t *testing.T) {
	empty := history.Result{Columns: []string{"source"}, Rows: [][]any{}}
	metadata, err := formatHistoryResult(historyContext{}, empty, 10000)
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, utf8.RuneCountInString(metadata) - 1} {
		if _, err := formatHistoryResult(historyContext{}, empty, limit); err == nil {
			t.Fatal("metadata bypassed the configured result limit")
		}
	}
	empty.Rows = [][]any{{strings.Repeat("source", 100)}}
	if _, err := formatHistoryResult(historyContext{}, empty, utf8.RuneCountInString(metadata)+1); err == nil {
		t.Fatal("a required truncation notice bypassed the configured limit")
	}
}

func TestHistoryTruncationNoticeCanDisplaceSeveralAlreadyPackedShortRows(t *testing.T) {
	source := history.Result{Columns: []string{"n"}}
	for i := range 100 {
		source.Rows = append(source.Rows, []any{i})
	}
	full, err := formatHistoryResult(historyContext{}, source, 10000)
	if err != nil {
		t.Fatal(err)
	}
	limit := utf8.RuneCountInString(full) - 1
	body, err := formatHistoryResult(historyContext{}, source, limit)
	if err != nil || utf8.RuneCountInString(body) > limit {
		t.Fatalf("new truncation metadata displaced the result budget: %s %v", body, err)
	}
	got := decodeHistoryFormat(t, body)
	rows := got["rows"].([]any)
	if got["truncated"] != true || len(rows) == 0 || len(rows) >= 98 || got["row_count"].(json.Number).String() != stringInt(len(rows)) {
		t.Fatalf("notice did not remove enough complete rows: %#v", got)
	}
	for i, value := range rows {
		if value.(map[string]any)["n"].(json.Number).String() != stringInt(i) {
			t.Fatalf("packed prefix was not preserved: %#v", rows)
		}
	}
}

func TestHistorySchemaSemanticsDescribeOnlyExistingCanonicalFields(t *testing.T) {
	source := history.Result{Schema: []history.Table{
		{Name: "messages", Columns: []history.Column{{Name: "id"}, {Name: "seq"}, {Name: "role"}, {Name: "source"}, {Name: "unrelated"}}},
		{Name: "events", Columns: []history.Column{{Name: "message_id"}}},
		{Name: "alias", Columns: []history.Column{{Name: "id"}}},
	}}
	body, err := formatHistoryResult(historyContext{}, source, 10000)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeHistoryFormat(t, body)
	meanings := got["field_semantics"].(map[string]any)
	if len(meanings) != 5 || !strings.Contains(meanings["messages.id"].(string), "Global") || !strings.Contains(meanings["messages.seq"].(string), "within one conversation") || !strings.Contains(meanings["events.message_id"].(string), "Streamed model-output identity") || !strings.Contains(meanings["messages.source"].(string), "human authorship") {
		t.Fatalf("canonical identity distinctions missing: %#v", meanings)
	}
	if len(got["rows"].([]any)) != 0 || len(got["columns"].([]any)) != 0 || len(got["schema"].([]any)) != len(source.Schema) {
		t.Fatalf("schema discovery changed shape: %#v", got)
	}
}

func TestHistoryToolKeepsActualDuplicateSQLProjectionsAndEmptyTypedValues(t *testing.T) {
	_, query, _ := historyToolFixture(t)
	for _, sql := range []string{
		`SELECT 1 AS id,2 AS id`,
		`SELECT 1 AS id,2 AS ID,3 AS Id`,
		`SELECT 1 AS id,2 AS "id:1",3 AS id,4 AS "id:2",5 AS ID`,
		`SELECT 1 AS "",2 AS "",3 AS ":1"`,
	} {
		args, _ := json.Marshal(map[string]any{"sql": sql})
		body, err := query.InvokableRun(context.Background(), string(args))
		if err != nil {
			t.Fatal(err)
		}
		got := decodeHistoryFormat(t, body)
		row := got["rows"].([]any)[0].(map[string]any)
		columns := got["columns"].([]any)
		if len(row) != len(columns) {
			t.Fatalf("duplicate SQL projections were lost: %s", body)
		}
		for i, name := range columns {
			if row[name.(string)].(json.Number).String() != stringInt(i+1) {
				t.Fatalf("SQLite's unique labels lost their original values: %s", body)
			}
		}
	}
	body, err := query.InvokableRun(context.Background(), `{"sql":"SELECT '' AS text,x'' AS blob,NULL AS absent,? AS original","params":["before\u0000after"]}`)
	if err != nil {
		t.Fatal(err)
	}
	row := decodeHistoryFormat(t, body)["rows"].([]any)[0].(map[string]any)
	if row["text"] != "" || row["blob"] != "" || row["absent"] != nil || row["original"] != "before\x00after" {
		t.Fatalf("empty values or NUL changed during wrapping: %s", body)
	}
}

func TestHistoryToolReturnsRepairableSchemaFormattingFailure(t *testing.T) {
	_, query, _ := historyToolFixture(t)
	raw, err := query.reader.Query(context.Background(), "", nil, query.maxChars)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := json.Marshal(struct {
		OK      bool           `json:"ok"`
		Context historyContext `json:"context"`
		history.Result
	}{true, query.context, raw})
	query.maxChars = utf8.RuneCount(old) + 16
	got := historyToolReply(t, query, context.Background(), `{}`)
	if got["ok"] != false || got["error"].(map[string]any)["code"] != "result_format_failed" {
		t.Fatalf("schema metadata failure was hidden: %#v", got)
	}
}

package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func reportRequestFixture() historyReportRequest {
	return historyReportRequest{Version: 1, Language: "zh",
		Sources:   []historyReportReference{{Key: "first", Source: store.HistoryReference{Kind: "message", ThreadID: "threadA", Seq: 2}}},
		Proposals: []historyReportProposal{{Title: "A bounded proposal", Priority: "P1", Disposition: "include", Proposal: "Consider an alternative; it still needs validation.", Sources: []string{"first"}}},
		Questions: []string{"Which assumption needs checking?"}}
}

func reportSourceFixture() store.HistorySource {
	return store.HistorySource{Reference: store.HistoryReference{Kind: "message", ThreadID: "threadA", Seq: 2},
		RowID: 71, ThreadTitle: "Recorded discussion", ProjectID: "projectA", Role: "assistant", Source: "",
		AgentID: "manager", Text: "Do not claim this is approved.\x00Only when the condition holds.\n```\n# Untrusted heading\n```",
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func reportJSON(t *testing.T, request historyReportRequest) string {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The protocol carries references and suggestions, never model-authored facts.
func TestHistoryReportDecodesOnlyOneStrictVersionedObject(t *testing.T) {
	want := reportRequestFixture()
	text := reportJSON(t, want)
	got, err := decodeHistoryReport(text, utf8.RuneCountInString(text))
	if err != nil || got.Sources[0] != want.Sources[0] || got.Proposals[0].Proposal != want.Proposals[0].Proposal {
		t.Fatalf("valid report refused or changed: %+v, %v", got, err)
	}
	for _, invalid := range []string{
		"null", "[]", "true", "{", text + " {}", text + " trailing", "```json\n" + text + "\n```",
		strings.Replace(text, `"version":1`, `"version":2`, 1),
		strings.Replace(text, `"language":"zh"`, `"language":"fr"`, 1),
		strings.Replace(text, `"version":1`, `"version":1,"summary":"asserted facts"`, 1),
		strings.Replace(text, `"key":"first"`, `"key":"first","quote":"trimmed"`, 1),
		strings.Replace(text, `"kind":"message"`, `"kind":"message","author":"human"`, 1),
		strings.Replace(text, `"title":"A bounded proposal"`, `"title":"A bounded proposal","status":"verified"`, 1),
		strings.Replace(text, `"version":1`, `"version":2,"version":1`, 1),
		strings.Replace(text, `"version":1`, `"Version":1`, 1),
		strings.Replace(text, `"version":1`, `"version":2,"Version":1`, 1),
		strings.Replace(text, `"thread_id":"threadA"`, `"THREAD_ID":"threadA"`, 1),
		strings.Replace(text, `"source":{"kind"`, `"source":{"seq":9,"kind"`, 1),
		string([]byte{'{', '"', 0xff, '"', ':', '1', '}'}),
	} {
		t.Run(invalid[:min(len(invalid), 55)], func(t *testing.T) {
			if _, err := decodeHistoryReport(invalid, 10000); err == nil {
				t.Fatalf("accepted ambiguous/unsupported report: %q", invalid)
			}
		})
	}
	if _, err := decodeHistoryReport(text, 0); err == nil {
		t.Fatal("accepted an absent input budget")
	}
	if _, err := decodeHistoryReport(text, utf8.RuneCountInString(text)-1); err == nil {
		t.Fatal("input budget ignored")
	}
}

func TestHistoryReportReferencesCannotAliasOrInventSources(t *testing.T) {
	tests := map[string]func(*historyReportRequest){
		"blank key":    func(r *historyReportRequest) { r.Sources[0].Key = " \t" },
		"unknown kind": func(r *historyReportRequest) { r.Sources[0].Source.Kind = "turn" },
		"empty thread": func(r *historyReportRequest) { r.Sources[0].Source.ThreadID = "\t" },
		"zero seq":     func(r *historyReportRequest) { r.Sources[0].Source.Seq = 0 },
		"negative seq": func(r *historyReportRequest) { r.Sources[0].Source.Seq = -1 },
		"duplicate key": func(r *historyReportRequest) {
			other := r.Sources[0]
			other.Source.Seq++
			r.Sources = append(r.Sources, other)
		},
		"duplicate canonical ref": func(r *historyReportRequest) {
			other := r.Sources[0]
			other.Key = "second"
			r.Sources = append(r.Sources, other)
		},
		"missing proposal source": func(r *historyReportRequest) { r.Proposals[0].Sources = []string{"missing"} },
		"invalid disposition":     func(r *historyReportRequest) { r.Proposals[0].Disposition = "approved" },
		"empty proposal title":    func(r *historyReportRequest) { r.Proposals[0].Title = " " },
		"empty proposal text":     func(r *historyReportRequest) { r.Proposals[0].Proposal = "\n" },
		"empty question":          func(r *historyReportRequest) { r.Questions = []string{" "} },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			r := reportRequestFixture()
			change(&r)
			if _, err := decodeHistoryReport(reportJSON(t, r), 10000); err == nil {
				t.Fatal("invalid reference/proposal accepted")
			}
			if _, err := renderHistoryReport(r, []store.HistorySource{reportSourceFixture()}, 10000); err == nil {
				t.Fatal("direct typed call bypassed validation")
			}
		})
	}
}

// A source is a whole attributed record, including empty text and image refs.
func TestHistoryReportRetainsWholeSourcesWithoutPromotingAuthority(t *testing.T) {
	r := reportRequestFixture()
	r.Sources = append(r.Sources, historyReportReference{Key: "event", Source: store.HistoryReference{Kind: "event", ThreadID: "threadB", Seq: 2}})
	sources := []store.HistorySource{reportSourceFixture(), {
		Reference: r.Sources[1].Source, RowID: 999, ThreadTitle: "Other recorded discussion", Role: "user", Source: "", AgentID: "workerA", EventKind: "tool_result", Err: "Not completed.\x00Only a failed observation.\n```\n",
		CreatedAt: time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC),
		Images:    []store.ImageRef{{ID: "imageA", Name: "image[1].png", MIME: "image/png"}},
	}}
	out, err := renderHistoryReport(r, sources, 30000)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{sources[0].Text, sources[1].Err, "message_id=71", "message_seq=2", "event_seq=2", "2026-01-02T03:04:05Z", "2026-01-03T03:04:05Z", "assistant", `tool\_result`, "workerA", "imageA", `image\/png`, "原文为空", "未解读", "不表示成功", "不是对整个项目的完整审阅", "建议纳入"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing source/limitation %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "message_id=999") || strings.Contains(out, "均为人类确认") || strings.Contains(out, "![") {
		t.Fatalf("event/image metadata gained false authority or image interpretation:\n%s", out)
	}
	if !strings.Contains(out, "````text\n"+sources[0].Text) {
		t.Fatal("source fence can be closed by source text")
	}
}

func TestHistoryReportDoesNotTrustShiftedOrMissingStoreResults(t *testing.T) {
	r := reportRequestFixture()
	good := reportSourceFixture()
	for _, sources := range [][]store.HistorySource{nil, {good, good}, {{Reference: store.HistoryReference{Kind: "message", ThreadID: "other", Seq: 2}}}} {
		if _, err := renderHistoryReport(r, sources, 10000); err == nil {
			t.Fatalf("mismatched source array accepted: %+v", sources)
		}
	}
}

func TestHistoryReportNewSuggestionsAreLabelledAndCannotInjectMarkdownHeadings(t *testing.T) {
	r := historyReportRequest{Version: 1, Language: "en", Proposals: []historyReportProposal{
		{Title: "\n## Human-approved\n[link](javascript:run)", Priority: "P1 | <b>verified</b>", Disposition: "include", Proposal: "\n<script>run()</script>\n**verified**\n~~~\n= authority ="},
		{Title: "Excluded proposal", Disposition: "exclude", Proposal: "Do not do this."},
		{Title: "Candidate proposal", Disposition: "candidate", Proposal: "This still needs validation."},
	}, Questions: []string{"\n# Authority\n![image](remote)"}}
	out, err := renderHistoryReport(r, nil, 10000)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Original evidence", "Suggested inclusion (1)", "Suggested exclusion (1)", "Unverified candidates (1)", "Open questions (1)", "No historical evidence", `\#\# Human\-approved`, `\<script\>`, `\*\*verified\*\*`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing plain-text boundary %q:\n%s", want, out)
		}
	}
	for _, forbidden := range []string{"\n## Human-approved\n", "<script>", "[link](javascript:", "**verified**", "![image](remote)", "\n# Authority"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("model text injected markup %q:\n%s", forbidden, out)
		}
	}
}

func TestHistoryReportEnglishUserRuntimeAndUndatedRecordsRemainAttributed(t *testing.T) {
	r := reportRequestFixture()
	r.Language = "en"
	r.Proposals[0].Sources = nil
	source := reportSourceFixture()
	source.Text = ""
	source.Role = "user"
	source.Source = "runtime"
	source.CreatedAt = time.Time{}
	out, err := renderHistoryReport(r, []store.HistorySource{source}, 10000)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"user", "runtime", "Date not recorded", "Empty original text", "No historical evidence", "does not establish approval"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing provenance/unknown %q", want)
		}
	}
}

func TestHistoryReportDatesDescribeOnlyTheCitedRecords(t *testing.T) {
	r := reportRequestFixture()
	r.Sources = append(r.Sources,
		historyReportReference{Key: "earlier", Source: store.HistoryReference{Kind: "message", ThreadID: "threadA", Seq: 3}},
		historyReportReference{Key: "undated", Source: store.HistoryReference{Kind: "message", ThreadID: "threadA", Seq: 4}})
	first := reportSourceFixture()
	earlier, undated := first, first
	earlier.Reference, undated.Reference = r.Sources[1].Source, r.Sources[2].Source
	earlier.CreatedAt = time.Date(2026, 1, 1, 4, 5, 6, 0, time.FixedZone("offset", 3600))
	undated.CreatedAt = time.Time{}
	r.Questions = nil
	out, err := renderHistoryReport(r, []store.HistorySource{first, earlier, undated}, 30000)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"3 条记录，来自 1 个会话", "2026-01-01T03:05:06Z 至 2026-01-02T03:04:05Z", "另有 1 条引用记录未记日期", "待确认问题 (0)"} {
		if !strings.Contains(out, want) {
			t.Errorf("wrong cited range or date attribution: missing %q", want)
		}
	}
}

func TestHistoryReportBudgetRejectsWholeOutputInsteadOfTrimmingSource(t *testing.T) {
	r := reportRequestFixture()
	s := reportSourceFixture()
	s.Text = strings.Repeat("完整原文\x00", 12)
	out, err := renderHistoryReport(r, []store.HistorySource{s}, 10000)
	if err != nil {
		t.Fatal(err)
	}
	chars := utf8.RuneCountInString(out)
	if exact, err := renderHistoryReport(r, []store.HistorySource{s}, chars); err != nil || exact != out {
		t.Fatalf("exact rune budget changed output: %v", err)
	}
	for _, budget := range []int{0, -1, chars - 1} {
		if partial, err := renderHistoryReport(r, []store.HistorySource{s}, budget); err == nil || partial != "" {
			t.Fatalf("returned partial/oversized report at budget %d", budget)
		}
	}
}

func TestHistoryReportPromptIsOnlyAGenericProtocol(t *testing.T) {
	prompt := historyReportPrompt()
	for _, want := range []string{"version", "language", "sources", "proposals", "questions", "thread_id", "seq", "include", "exclude", "candidate", "JSON", "role", "complete", "report_schedule.findings", "must not repeat", "must first call query_history", "future or conditional action", "short rank label", "Historical rationale belongs only in sources", "verify the existing implementation"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("protocol omitted %q", want)
		}
	}
	for _, forbidden := range []string{"fofa", "澄海", "潮汐", "glm", "fingerprint", "create_annotation"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("example-specific token leaked: %q", forbidden)
		}
	}
}

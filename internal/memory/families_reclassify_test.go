package memory

import "testing"

func TestTidyReportCountsMovesWithoutCallingThemMerges(t *testing.T) {
	report := ComposeTidyReport([]string{"kept", "demoted"}, []string{"kept", "promoted"}, 0, []Change{{Target: ToolSkillManage, Action: "promote", Name: "promoted"}, {Target: ToolSkillManage, Action: "demote", Name: "demoted"}})
	if report.Promoted != 1 || report.Demoted != 1 || !report.Folded() || len(report.Merged) != 0 || report.Unchanged != 1 {
		t.Fatalf("report=%+v", report)
	}
	retry := ComposeTidyReport([]string{"kept"}, []string{"kept"}, 0, []Change{{Target: ToolSkillManage, Action: "promote", Name: "kept"}})
	if retry.Promoted != 1 || !retry.Folded() || retry.Unchanged != 0 {
		t.Fatalf("partial retry=%+v", retry)
	}
}

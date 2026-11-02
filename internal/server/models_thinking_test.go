package server

import "testing"

// The dump carries pi's own level computation; the merge must key on
// the same provider/model ids the table rows use and leave unknown
// rows bare so the phone falls back to the full list.
func TestApplyThinkingDumpMergesByID(t *testing.T) {
	rows := []modelRow{
		{ID: "zai/glm-5.3-flash", Context: 1000000},
		{ID: "moonshotai/kimi-k2.6", Context: 262144},
		{ID: "openai/gpt-5.2"},
	}
	dump := []byte(`[
		{"id":"zai/glm-5.3-flash","levels":["low","high","max"]},
		{"id":"openai/gpt-5.2","levels":["off"]}
	]`)
	applyThinkingDump(rows, dump)
	if got := rows[0].Levels; len(got) != 3 || got[0] != "low" {
		t.Fatalf("glm-5.3-flash should carry pi's levels, got %v", got)
	}
	if rows[1].Levels != nil {
		t.Fatal("rows the dump does not know must stay bare")
	}
	if rows[2].Levels == nil || len(rows[2].Levels) != 1 || rows[2].Levels[0] != "off" {
		t.Fatalf("non-reasoning model should carry exactly off, got %v", rows[2].Levels)
	}
}

func TestApplyThinkingDumpToleratesJunk(t *testing.T) {
	rows := []modelRow{{ID: "zai/glm-5.2", Context: 1000000}}
	applyThinkingDump(rows, []byte(`not json`))
	if rows[0].Levels != nil {
		t.Fatal("junk dump must leave rows untouched")
	}
}

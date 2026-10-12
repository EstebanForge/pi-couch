package server

import (
	"strings"
	"testing"
)

func TestParseModelList(t *testing.T) {
	tsv := strings.Join([]string{
		"provider        model                     context  max-out  thinking  images",
		"anthropic       claude-sonnet-5           1M       128K     yes       yes",
		"openai          gpt-6.1                   400K     128K     yes       yes",
		"", // trailing newline
	}, "\n")
	got := parseModelList(tsv)
	if len(got) != 2 {
		t.Fatalf("rows %d: %+v", len(got), got)
	}
	if got[0].ID != "anthropic/claude-sonnet-5" || got[1].ID != "openai/gpt-6.1" {
		t.Fatalf("ids: %q %q", got[0].ID, got[1].ID)
	}
}

func TestParseModelListSkipsShortLines(t *testing.T) {
	tsv := "provider model context\n\nonlyonecolumn\nanthropic claude-sonnet-5 1M 128K yes yes"
	if got := parseModelList(tsv); len(got) != 1 {
		t.Fatalf("rows %d: %+v", len(got), got)
	}
}

func TestFilterModels(t *testing.T) {
	rows := []modelRow{
		{ID: "anthropic/claude-sonnet-5"},
		{ID: "anthropic/claude-opus-4-6"},
		{ID: "openai/gpt-6.1"},
	}
	got := filterModels(rows, "SONNET", 25)
	if len(got) != 1 || got[0].ID != "anthropic/claude-sonnet-5" {
		t.Fatalf("got %+v", got)
	}
	if all := filterModels(rows, "", 2); len(all) != 2 {
		t.Fatalf("uncapped: %+v", all)
	}
	if none := filterModels(rows, "zzz", 25); len(none) != 0 {
		t.Fatalf("none: %+v", none)
	}
}

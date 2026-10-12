package pages

import "testing"

func TestParseDiffHeaderVsContent(t *testing.T) {
	// "--- a/f" is header metadata; a removed "-- sql" line mid-hunk renders
	// as content; "\ No newline" never counts as a context line.
	in := "diff --git a/f b/f\n" +
		"index 111..222 100644\n" +
		"--- a/f\n" +
		"+++ b/f\n" +
		"@@ -1,3 +1,3 @@\n" +
		"-- sql\n" +
		"+-- sql\n" +
		" ctx\n" +
		"\\ No newline at end of file\n"
	got := parseDiff(in)
	kinds := ""
	for _, l := range got {
		kinds += l.kind[0:1]
	}
	// diff--git ctx, index ctx, --- meta, +++ meta, hunk, del, add, ctx, meta(no-newline)
	want := "ccmmhdacm"
	if kinds != want {
		t.Fatalf("kinds: got %q, want %q (%+v)", kinds, want, got)
	}
	// The removed "-- sql" line stays a deletion with its content.
	del := got[5]
	if del.kind != "del" || del.text != "-- sql" {
		t.Errorf("mid-hunk deletion: %+v", del)
	}
	// The no-newline marker must not add a row or advance the counter.
	ctx2 := got[7]
	if ctx2.kind != "ctx" || ctx2.no != 3 {
		t.Errorf("context after marker: %+v", ctx2)
	}
}

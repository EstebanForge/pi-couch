package server

import (
	"regexp"
	"strings"
)

// parsePiDialog reads one blocking pi dialog off a pane screen: the
// hint bar at the end (above pi's own footer) names the widget kind,
// the rows above it are what its arrows move through, and the lines
// above the blank are what the dialog asks. This is herdr-web-ui's
// parsePiDialog discipline (server/prompt.ts), narrowed to pi's hint
// family: the rpiv ask-user questionnaire, the extension editor, the
// plain select/confirm menu and the input line. pi's /model picker
// draws rows too but its hint names Ctrl+S to save a default, so it
// must never parse - answering that from a card would switch pi's
// default model.

type piDialogOption struct {
	Label       string
	Description string
}

type piDialog struct {
	Kind    string // "select", "editor" or "input"
	Title   string
	Body    string
	Options []piDialogOption
	Cursor  int
	// InputMode marks the questionnaire's free-text state: the cursor
	// sits on the "Type something." row and its label is now the live
	// draft. The answer flow keys its phase transition on this.
	InputMode bool
}

var (
	// The rpiv ask-user questionnaire: "Enter to select · ↑/↓ to
	// navigate · n to add notes · Esc to cancel · Ctrl+] to collapse".
	piAskHintRe = regexp.MustCompile(`(?i)enter to select\s*[·•]\s*↑/↓ to navigate(\s*[·•]\s*(space to toggle|n to add notes))*\s*[·•]\s*esc to cancel`)
	// The extension editor: "enter submit  shift+enter/ctrl+j newline
	// escape/ctrl+c cancel  ctrl+g external editor".
	piEditorHintRe = regexp.MustCompile(`(?i)enter\s+submit\s+shift\+enter/ctrl\+j\s+newline\s+escape/ctrl\+c\s+cancel`)
	// The questionnaire's free-text row: selecting it opens pi's input
	// widget, so the card renders a text field in its place.
	piOwnAnswerRe = regexp.MustCompile(`(?i)^type something`)
	// The same questionnaire once the cursor reaches that row: the row
	// label is replaced by the live draft and the hint gains the
	// newline/clear segments. Captured live 2026-10-17.
	piAskInputHintRe = regexp.MustCompile(`(?i)enter to select\s*[·•]\s*↑/↓ to navigate\s*[·•]\s*esc to cancel.*shift\+enter for newline`)
	// pi's plain menu and input hints, shared with awaiting.go.
	// piRowCursorRe marks the row pi's cursor sits on.
	piRowCursorRe  = regexp.MustCompile(`^[→❯›➜]\s*\S`)
	piCursorMarkRe = regexp.MustCompile(`^[→❯›➜]\s*`)
	piNumberedRe   = regexp.MustCompile(`^(\d+)[.)]\s+(.*)$`)
	piDividerRe    = regexp.MustCompile(`^\s*[-─━═_]{4,}\s*$`)
)

// piHintKind scans the trailing lines for one of pi's hint bars, the
// way piHintAtEnd does but returning which family matched and where
// its block starts. A phone-width pane wraps the hint, and pi paints
// its footer (folder, context meter) under every dialog, so joins of
// the trailing lines carry the match.
func piHintKind(plain string) (kind string, hintIndex int) {
	lines := strings.Split(plain, "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	const window = 6
	start := end - window
	if start < 0 {
		start = 0
	}
	// Highest window first: its first line is the hint itself. A lower
	// window would swallow the dialog's rows and prose into the join.
	for i := end - 1; i >= start; i-- {
		joined := strings.Join(lines[i:end], " ")
		switch {
		case piEditorHintRe.MatchString(joined):
			return "editor", i
		case piMenuHintRe.MatchString(joined):
			return "select", i
		case piAskInputHintRe.MatchString(joined):
			return "askinput", i
		case piAskHintRe.MatchString(joined):
			return "select", i
		case piInputHintRe.MatchString(joined):
			return "input", i
		}
	}
	return "", -1
}

// parsePiDialog extracts the dialog a pane shows, or nil when no pi
// dialog hint anchors the screen.
func parsePiDialog(text string) *piDialog {
	if text == "" {
		return nil
	}
	plain := ansiRe.ReplaceAllString(text, "")
	kind, hint := piHintKind(plain)
	if hint < 0 {
		return nil
	}
	lines := strings.Split(plain, "\n")
	if kind == "editor" {
		return parsePiEditorBlock(lines[:hint])
	}
	if kind == "input" {
		return parsePiInputBlock(lines[:hint])
	}
	if kind == "askinput" {
		d := parsePiRowsBlock(lines[:hint], "select")
		if d != nil {
			d.InputMode = true
		}
		return d
	}
	return parsePiRowsBlock(lines[:hint], kind)
}

// parsePiInputBlock reads pi's free-text widget: a question head above
// the hint and optionally a >-led line holding the current draft. No
// rows and no cursor - that is what separates it from a menu whose
// text merely sits over an input hint.
func parsePiInputBlock(lines []string) *piDialog {
	var head []string
	for i := len(lines); i > 0 && len(head) < 8; i-- {
		line := strings.TrimSpace(lines[i-1])
		if line == "" {
			if len(head) > 0 {
				break
			}
			continue
		}
		if piDividerRe.MatchString(line) {
			break
		}
		if strings.HasPrefix(line, ">") {
			continue
		}
		head = append([]string{line}, head...)
	}
	d := &piDialog{Kind: "input"}
	if len(head) > 0 {
		d.Title = head[0]
		d.Body = strings.Join(head[1:], "\n")
	}
	return d
}

// parsePiEditorBlock reads the editor overlay: title and body sit in
// segments between pi's divider rules above the hint. Reading upward,
// the first segment is the body (the draft text), the next the title.
func parsePiEditorBlock(lines []string) *piDialog {
	var segments [][]string
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if piDividerRe.MatchString(line) || line == "" {
			// Blanks pad between pi's rules; a segment boundary is a rule
			// (or blank) met with text already collected.
			if len(segments) > 0 && len(segments[len(segments)-1]) > 0 {
				if len(segments) == 2 {
					break
				}
				segments = append(segments, nil)
			}
			continue
		}
		if len(segments) == 0 {
			segments = append(segments, nil)
		}
		cur := segments[len(segments)-1]
		segments[len(segments)-1] = append([]string{line}, cur...)
	}
	if len(segments) == 0 || len(segments[0]) == 0 {
		return nil
	}
	d := &piDialog{Kind: "editor", Body: strings.Join(segments[0], "\n")}
	if len(segments) > 1 && len(segments[1]) > 0 {
		d.Title = segments[1][0]
		d.Body = strings.Join(append(append([]string{}, segments[1][1:]...), segments[0]...), "\n")
	}
	return d
}

// parsePiRowsBlock reads a menu-shaped dialog reading upward from the
// hint: rows are cursor-marked lines, numbered lines (the rpiv
// questionnaire) or pi-tui's two-space plain rows; a deeper-indented
// non-row is the row below it's description (pi paints it under its
// row, so reading upward it arrives first and buffers); anything else
// ends the block, and the run above it is the title and body.
func parsePiRowsBlock(lines []string, kind string) *piDialog {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	var opts []piDialogOption
	var rest []string // painted lines met before the row they belong to
	belowAtMark := -1
	boundary := end
	for i := end; i > 0 && len(opts) < 32; i-- {
		raw := lines[i-1]
		line := strings.TrimSpace(raw)
		if line == "" || piDividerRe.MatchString(line) {
			if len(opts) > 0 {
				boundary = i
				goto done
			}
			end = i - 1
			continue
		}
		label, numbered, isRow := piRowLabel(raw)
		if !isRow {
			if len(opts) > 0 || len(rest) > 0 {
				// Painted between rows and hint: it belongs to whichever
				// row the next (lower) one turns out to be.
				rest = append(rest, line)
				continue
			}
			end = i - 1
			continue
		}
		if numbered {
			opts = append([]piDialogOption{{Label: label, Description: strings.Join(rest, " ")}}, opts...)
		} else if len(rest) > 0 {
			// A plain menu's wrapped option rest continues the label.
			opts = append([]piDialogOption{{Label: strings.Join(append([]string{label}, rest...), " ")}}, opts...)
			rest = nil
		} else {
			opts = append([]piDialogOption{{Label: label}}, opts...)
		}
		rest = nil
		if piRowCursorRe.MatchString(strings.TrimLeft(raw, " ")) {
			// Rows below the marked one are already collected; its final
			// index is the list's end minus them.
			belowAtMark = len(opts) - 1
		}
		boundary = i - 1
	}
done:
	if len(opts) < 2 || belowAtMark < 0 {
		// A menu is at least two rows and pi always paints its cursor;
		// anything else is prose that happens to sit over a hint.
		return nil
	}
	d := &piDialog{Kind: kind, Options: opts, Cursor: len(opts) - 1 - belowAtMark}
	// Title and body: the non-blank run above the rows, first line the
	// title, bounded by a blank or pi's divider.
	var head []string
	for i := boundary; i > 0 && len(head) < 8; i-- {
		line := strings.TrimSpace(lines[i-1])
		if line == "" {
			if len(head) > 0 {
				break
			}
			continue
		}
		if piDividerRe.MatchString(line) {
			break
		}
		head = append([]string{line}, head...)
	}
	if len(head) > 0 {
		d.Title = head[0]
		d.Body = strings.Join(head[1:], "\n")
	}
	return d
}

// piRowLabel strips pi's row chrome and says whether the line is a
// row at all: cursor-marked lines and numbered lines are rows at any
// indent; a plain label is a row only at pi-tui's two-space indent
// (the questionnaire's question lines sit at one, its descriptions
// deeper). A two-column line (the slash palette) is not a row.
func piRowLabel(raw string) (label string, numbered bool, isRow bool) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return "", false, false
	}
	indent := indentOf(raw)
	cursor := piCursorMarkRe.MatchString(line)
	if cursor {
		line = strings.TrimSpace(piCursorMarkRe.ReplaceAllString(line, ""))
	}
	if m := piNumberedRe.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[2]), true, true
	}
	if cursor {
		return line, false, true
	}
	if indent != 2 || line == "" || strings.Contains(line, "  ") {
		return "", false, false
	}
	return line, false, true
}

// indentOf counts a line's leading spaces.
func indentOf(raw string) int {
	return len(raw) - len(strings.TrimLeft(raw, " "))
}

// piDialogEqual reports whether two parses carry the same dialog
// content. Pointers always differ between parses, so the detector's
// change detection needs a value comparison; option order matters (it
// drives the answer walk).
func piDialogEqual(a, b *piDialog) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind || a.Title != b.Title || a.Body != b.Body ||
		a.Cursor != b.Cursor || a.InputMode != b.InputMode || len(a.Options) != len(b.Options) {
		return false
	}
	for i := range a.Options {
		if a.Options[i] != b.Options[i] {
			return false
		}
	}
	return true
}

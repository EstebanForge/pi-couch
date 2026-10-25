package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Pi advertises skills and prompt templates as slash commands; these
// scans mirror pi's discovery so the phone offers the same set as the
// terminal. Not followed: package-bundled resources and settings glob
// syntax (plain paths only).

type piSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type piPrompt struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Hint        string `json:"hint,omitempty"`
}

func (a *app) apiSkills(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"skills": scanPiSkills(a.home, r.URL.Query().Get("cwd"))})
}

func (a *app) apiPrompts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"prompts": scanPiPrompts(a.home, r.URL.Query().Get("cwd"))})
}

// skillRoots lists pi's user skill dirs (conventional plus settings),
// then the project dirs found walking from cwd up to its repo root.
func skillRoots(home, cwd string) []string {
	roots := []string{
		filepath.Join(home, ".pi", "agent", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}
	for _, p := range settingsResourcePaths(home, "skills") {
		roots = append(roots, p)
	}
	for _, dir := range projectWalk(home, cwd) {
		roots = append(roots,
			filepath.Join(dir, ".pi", "skills"),
			filepath.Join(dir, ".agents", "skills"),
		)
	}
	return roots
}

// promptRoots mirrors skillRoots for prompt-template dirs; pi loads
// direct .md children only, so a root is scanned one level deep.
func promptRoots(home, cwd string) []string {
	roots := []string{filepath.Join(home, ".pi", "agent", "prompts")}
	for _, p := range settingsResourcePaths(home, "prompts") {
		roots = append(roots, p)
	}
	for _, dir := range projectWalk(home, cwd) {
		roots = append(roots, filepath.Join(dir, ".pi", "prompts"))
	}
	return roots
}

func scanPiSkills(home, cwd string) []piSkill {
	found := map[string]string{}
	add := func(name, desc string) {
		if name == "" || desc == "" {
			return
		}
		if _, ok := found[name]; !ok {
			found[name] = desc
		}
	}
	for _, root := range skillRoots(home, cwd) {
		// pi's skill sets commonly symlink dirs into a central repo, so
		// the walk must stat through symlinks (DirFS will not); the depth
		// cap bounds symlink loops.
		var walk func(dir string, depth int)
		walk = func(dir string, depth int) {
			if depth > 6 {
				return
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, e := range entries {
				p := filepath.Join(dir, e.Name())
				info, statErr := os.Stat(p)
				if statErr != nil || !info.IsDir() {
					continue
				}
				raw, readErr := os.ReadFile(filepath.Join(p, "SKILL.md"))
				if readErr != nil {
					walk(p, depth+1)
					continue
				}
				name, desc := piFrontmatter(string(raw))
				if name == "" {
					name = e.Name()
				}
				add(name, desc)
				// a dir holding SKILL.md is one skill; pi does not descend
			}
		}
		walk(root, 0)
	}
	out := make([]piSkill, 0, len(found))
	for name, desc := range found {
		out = append(out, piSkill{Name: name, Description: desc})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func scanPiPrompts(home, cwd string) []piPrompt {
	found := map[string]piPrompt{}
	add := func(p piPrompt) {
		if p.Name == "" || p.Description == "" {
			return
		}
		if _, ok := found[p.Name]; !ok {
			found[p.Name] = p
		}
	}
	for _, root := range promptRoots(home, cwd) {
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			if p, ok := piPromptFile(root); ok {
				add(p)
			}
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if p, ok := piPromptFile(filepath.Join(root, e.Name())); ok {
				add(p)
			}
		}
	}
	out := make([]piPrompt, 0, len(found))
	for _, p := range found {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func piPromptFile(path string) (piPrompt, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return piPrompt{}, false
	}
	fm, body := piFrontmatterMap(string(raw))
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	desc := fm["description"]
	if desc == "" {
		for _, line := range strings.Split(body, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				desc = line
				break
			}
		}
	}
	return piPrompt{Name: name, Description: desc, Hint: fm["argument-hint"]}, desc != ""
}

// piFrontmatter returns a SKILL.md's declared name and description;
// a file without frontmatter, name, or description never loads in pi,
// so both come back empty and the caller skips it.
func piFrontmatter(body string) (name, desc string) {
	fm, _ := piFrontmatterMap(body)
	return fm["name"], fm["description"]
}

func piFrontmatterMap(body string) (map[string]string, string) {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return map[string]string{}, body
	}
	fm := map[string]string{}
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			return fm, strings.Join(lines[i+1:], "\n")
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			v = strings.TrimSpace(v)
			// YAML-style quoted values keep their quotes in a naive parse.
			if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
			fm[strings.TrimSpace(k)] = v
		}
	}
	return map[string]string{}, body
}

// settingsResourcePaths reads the settings.json skills/prompts arrays.
// Entries resolve from the agent directory like pi resolves them;
// glob-syntax entries and unreadable settings are skipped.
func settingsResourcePaths(home, key string) []string {
	raw, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "settings.json"))
	if err != nil {
		return nil
	}
	var cfg struct {
		Skills  []string `json:"skills"`
		Prompts []string `json:"prompts"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	paths := cfg.Skills
	if key == "prompts" {
		paths = cfg.Prompts
	}
	agentDir := filepath.Join(home, ".pi", "agent")
	var out []string
	for _, p := range paths {
		if p == "" || strings.ContainsAny(p, "*?[") {
			continue
		}
		p = filepath.FromSlash(p)
		if !filepath.IsAbs(p) {
			if strings.HasPrefix(p, "~") {
				p = filepath.Join(home, p[1:])
			} else {
				p = filepath.Join(agentDir, p)
			}
		}
		out = append(out, p)
	}
	return out
}

// projectWalk walks from cwd up to its repository root, inclusive;
// dirs outside home or with no cwd yield nothing. Repo root detection
// matches pi's: the first ancestor carrying a .git directory.
func projectWalk(home, cwd string) []string {
	if cwd == "" || home == "" {
		return nil
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil
	}
	home, err = filepath.Abs(home)
	if err != nil || !strings.HasPrefix(cwd, home+string(filepath.Separator)) {
		return nil
	}
	var dirs []string
	for {
		dirs = append(dirs, cwd)
		if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
			return dirs
		}
		if cwd == home {
			return dirs
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return dirs
		}
		cwd = parent
	}
}

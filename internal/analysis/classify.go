package analysis

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

// Category is the behavioral meaning of a tool call, independent of the tool
// that happened to implement it (a Bash `cat` is a read, just like Read).
type Category string

const (
	CatRead   Category = "read"
	CatSearch Category = "search"
	CatEdit   Category = "edit"
	CatTest   Category = "test"
	CatMCP    Category = "mcp"
	CatShell  Category = "shell"
	CatAgent  Category = "subagent"
	CatSkill  Category = "skill"
	CatPlan   Category = "plan"
	CatWeb    Category = "web"
	CatOther  Category = "other"
)

// Action is a classified tool call.
type Action struct {
	Category Category
	Files    []string // absolute paths read or edited
	Broad    bool     // search without a narrowing path or with a repo-wide pattern
	Target   string   // short human label: path, pattern, command, MCP tool
}

var testCmd = regexp.MustCompile(`\b(go test|gotestsum|pytest|python -m pytest|npm (run )?test|yarn test|pnpm test|jest|vitest|mvn\b.*\btest|gradle\w*\b.*\btest|cargo test|make test|rspec|phpunit)\b`)

// Classify maps a tool call to a behavioral category. cwd resolves relative paths.
func Classify(t *session.ToolCall, cwd string) Action {
	if server, tool, ok := t.MCP(); ok {
		return Action{Category: CatMCP, Target: server + "." + tool + " " + canonical(t.Input)}
	}
	switch t.Name {
	case "Read":
		p := abs(t.Str("file_path"), cwd)
		return Action{Category: CatRead, Files: []string{p}, Target: p}
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		p := abs(firstNonEmpty(t.Str("file_path"), t.Str("notebook_path")), cwd)
		return Action{Category: editCategory(p), Files: []string{p}, Target: p}
	case "Grep":
		path := t.Str("path")
		return Action{Category: CatSearch, Target: t.Str("pattern"), Broad: path == "" || path == "." || path == cwd}
	case "Glob":
		pat := t.Str("pattern")
		return Action{Category: CatSearch, Target: pat, Broad: t.Str("path") == "" && strings.HasPrefix(pat, "**")}
	case "Agent", "Task":
		return Action{Category: CatAgent, Target: firstNonEmpty(t.Str("subagent_type"), "general") + ": " + t.Str("description")}
	case "Skill":
		return Action{Category: CatSkill, Target: t.Str("skill")}
	case "TodoWrite", "EnterPlanMode", "ExitPlanMode":
		return Action{Category: CatPlan, Target: t.Name}
	case "WebFetch", "WebSearch":
		return Action{Category: CatWeb, Target: firstNonEmpty(t.Str("url"), t.Str("query"))}
	case "Bash":
		return classifyBash(t.Str("command"), cwd)
	}
	return Action{Category: CatOther, Target: t.Name}
}

// classifyBash recognizes the common shell equivalents of read, search,
// edit and test. Precedence: test > edit > read > search > shell.
// ponytail: whitespace tokenizer, no real shell parsing; quoted paths with
// spaces are missed. Swap in mvdan.cc/sh if that matters.
func classifyBash(cmd, cwd string) Action {
	a := Action{Category: CatShell, Target: cmd}
	if testCmd.MatchString(cmd) {
		a.Category = CatTest
		return a
	}
	// A heredoc body is data (a script or file content), not commands.
	head, body := cmd, ""
	if strings.Contains(cmd, "<<") {
		head, body, _ = strings.Cut(cmd, "\n")
	}

	var reads, edits []string
	var search, scriptWrites bool
	for _, seg := range splitCommands(stripQuoted(head)) {
		f := strings.Fields(seg)
		if len(f) == 0 {
			continue
		}
		edits = append(edits, redirectTargets(f)...)
		switch filepath.Base(f[0]) {
		case "cat", "head", "tail", "less", "bat", "nl", "wc":
			reads = append(reads, pathArgs(f[1:])...)
		case "sed":
			if slices.ContainsFunc(f, func(s string) bool { return strings.HasPrefix(s, "-i") }) {
				edits = append(edits, pathArgs(f[1:])...)
			} else if slices.Contains(f, "-n") {
				reads = append(reads, pathArgs(f[1:])...)
			}
		case "rm", "mv", "tee", "touch", "patch":
			edits = append(edits, pathArgs(f[1:])...)
		case "git":
			if len(f) > 1 && (f[1] == "rm" || f[1] == "mv" || f[1] == "apply") {
				edits = append(edits, pathArgs(f[2:])...)
			}
		case "python", "python3", "node", "ruby", "perl":
			scriptWrites = scriptWrites || writesFiles.MatchString(body)
		case "grep", "rg", "ag", "find", "fd", "ls", "tree":
			search = true
			a.Broad = a.Broad || broadShellSearch(f)
		}
	}

	switch {
	case len(edits) > 0 || scriptWrites:
		a.Files = absAll(dedupe(edits), cwd)
		a.Category = CatEdit
		if len(a.Files) > 0 {
			a.Category = editCategory(a.Files...)
		}
	case len(reads) > 0:
		a.Category, a.Files = CatRead, absAll(dedupe(reads), cwd)
	case search:
		a.Category = CatSearch
	}
	return a
}

// writesFiles matches file-writing calls inside inline scripts.
var writesFiles = regexp.MustCompile(`write_text\(|write_bytes\(|open\([^)]*['"][wa]b?['"]|\.save\(|writeFileSync\(|os\.remove\(|unlink\(`)

// redirectTargets returns files written by > or >> (not 2>&1, /dev/null).
func redirectTargets(f []string) []string {
	var out []string
	for i, tok := range f {
		var target string
		switch {
		case tok == ">" || tok == ">>":
			if i+1 < len(f) {
				target = f[i+1]
			}
		case strings.HasPrefix(tok, ">"):
			target = strings.TrimLeft(tok, ">")
		}
		if target != "/dev/null" && looksLikePath(target) {
			out = append(out, target)
		}
	}
	return out
}

// editCategory keeps agent bookkeeping (plans, scratch files) out of the
// project edits, since "first edit" anchors several metrics.
func editCategory(paths ...string) Category {
	for _, p := range paths {
		if !strings.Contains(p, "/.claude/plans/") && !IsScratch(p) {
			return CatEdit
		}
	}
	if strings.Contains(paths[0], "/.claude/plans/") {
		return CatPlan
	}
	return CatShell
}

// IsScratch reports agent scratch space (temp dirs, scratchpads), which is not project code.
func IsScratch(p string) bool {
	return strings.HasPrefix(p, "/tmp/") || strings.HasPrefix(p, "/private/tmp/") || strings.Contains(p, "/scratchpad/")
}

func pathArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if looksLikePath(a) {
			out = append(out, a)
		}
	}
	return out
}

func absAll(ps []string, cwd string) []string {
	for i, p := range ps {
		ps[i] = abs(p, cwd)
	}
	return ps
}

func dedupe(ps []string) []string {
	slices.Sort(ps)
	return slices.Compact(ps)
}

// stripQuoted replaces quoted strings with a placeholder so patterns such as
// 'a\|b' do not look like pipes or paths.
func stripQuoted(cmd string) string {
	var b strings.Builder
	var quote rune
	for _, r := range cmd {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
		case r == '\'' || r == '"':
			quote = r
			b.WriteString("Q")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func splitCommands(cmd string) []string {
	return strings.FieldsFunc(cmd, func(r rune) bool { return r == '|' || r == ';' || r == '&' || r == '\n' })
}

func broadShellSearch(f []string) bool {
	recursive := f[0] == "rg" || f[0] == "find" || f[0] == "tree" || f[0] == "fd" || f[0] == "ag"
	var paths []string
	for _, a := range f[1:] {
		if strings.HasPrefix(a, "-") && strings.ContainsAny(a, "rR") && (f[0] == "grep" || f[0] == "ls") {
			recursive = true
		}
		if !strings.HasPrefix(a, "-") {
			paths = append(paths, a)
		}
	}
	if !recursive {
		return false
	}
	// grep/rg take the pattern first; find/tree/fd take the path first.
	if f[0] == "grep" || f[0] == "rg" || f[0] == "ag" {
		if len(paths) > 0 {
			paths = paths[1:]
		}
	}
	return len(paths) == 0 || paths[0] == "." || paths[0] == "./" || paths[0] == "/"
}

func looksLikePath(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") || strings.ContainsAny(s, `'"$<>*`) {
		return false
	}
	return strings.Contains(s, "/") || filepath.Ext(s) != ""
}

// IsTestFile reports whether a path looks like a test source.
func IsTestFile(p string) bool {
	b := filepath.Base(p)
	return strings.HasSuffix(b, "_test.go") || strings.HasPrefix(b, "test_") || strings.HasSuffix(b, "_test.py") ||
		strings.Contains(b, ".test.") || strings.Contains(b, ".spec.") ||
		(strings.HasSuffix(b, "Test.java") || strings.HasSuffix(b, "Tests.java")) ||
		strings.Contains(p, "/tests/") || strings.Contains(p, "/test/")
}

func abs(p, cwd string) string {
	if p == "" || filepath.IsAbs(p) || cwd == "" {
		return p
	}
	return filepath.Join(cwd, p)
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

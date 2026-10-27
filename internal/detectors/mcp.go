package detectors

import (
	"regexp"
	"strings"

	"github.com/NiharRan/unclog/internal/engine"
)

// Process matching for AI tooling. Rules are data below; Match runs them in order.
// Windsurf is rebranding to Devin on macOS (Devin.app); its processes are not matched yet.

var (
	mcpRe = regexp.MustCompile(`(?i)(@modelcontextprotocol/|mcp[-_]server|server[-_]mcp|(^|[\s/\\_.@=:-])mcp([\s/\\_.@=:-]|$))`)
	lspRe = regexp.MustCompile(`(?i)(typescript-language-server|tsserver|pyright|gopls|rust-analyzer)`)
	devRe = regexp.MustCompile(`(?i)((^|[\s/\\])vite(\.js)?(\s|$)|(^|[\s/\\])next(\.js)?\s+dev(\s|$)|next-server|(^|[\s/\\])webpack(-dev-server|\.js)?(\s+(serve|watch)|$))`)

	// The native Claude Code binary reports its version as the process name (e.g. "2.1.311").
	versionNameRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)
	claudeCodeRe  = regexp.MustCompile(`@anthropic-ai/claude-code`)
	claudeDirRe   = regexp.MustCompile(`[/\\]\.claude[/\\]`)
)

var runtimeNames = map[string]bool{
	"node": true, "nodejs": true, "bun": true, "deno": true, "uv": true, "uvx": true,
	"npx": true, "npm": true, "pnpm": true, "tsx": true,
}

var lspNames = map[string]bool{
	"typescript-language-server": true, "tsserver": true, "pyright": true, "pyright-langserver": true,
	"gopls": true, "rust-analyzer": true,
}

var devRuntimes = map[string]bool{"node": true, "nodejs": true, "bun": true, "deno": true}

func norm(name string) string { return strings.TrimSuffix(strings.ToLower(name), ".exe") }

func isRuntime(n string) bool { return runtimeNames[n] || strings.HasPrefix(n, "python") }

// appTool maps a desktop app process name ("Cursor", "Code Helper (Renderer)") to its tool.
// Prefix matching needs the space so Apple's "CursorUIViewService" is not Cursor.
func appTool(n string) string {
	for name, tool := range map[string]string{"cursor": "Cursor", "code": "VS Code", "windsurf": "Windsurf"} {
		if n == name || strings.HasPrefix(n, name+" ") {
			return tool
		}
	}
	return ""
}

// Interesting reports whether a process with this name could match, so the snapshot
// only fetches cmdline and memory for those.
func Interesting(name string) bool {
	n := norm(name)
	return isRuntime(n) || lspNames[n] || n == "ollama" || n == "claude" || versionNameRe.MatchString(n) || appTool(n) != ""
}

// isClaudeBinary is true when the process is the claude CLI, whatever its name field says.
func isClaudeBinary(n, cmd string) bool {
	if n == "claude" {
		return true
	}
	first, _, _ := strings.Cut(strings.TrimSpace(cmd), " ")
	first = strings.ReplaceAll(first, `\`, "/")
	return versionNameRe.MatchString(n) && norm(first[strings.LastIndex(first, "/")+1:]) == "claude"
}

// MatchAIProcess decides whether p belongs to an AI tool.
// Candidate is false for things that are legitimately long-lived on their own:
// desktop app main processes and the Ollama service.
func MatchAIProcess(p engine.Proc) (engine.ProcMatch, bool) {
	n, cmd := norm(p.Name), p.Cmdline
	if tool := appTool(n); tool != "" {
		helper := strings.Contains(n, " helper") || strings.Contains(cmd, " --type=")
		return engine.ProcMatch{Tool: tool, Candidate: helper}, true
	}
	switch {
	case n == "ollama":
		return engine.ProcMatch{Tool: "Ollama"}, true
	case isClaudeBinary(n, cmd):
		return engine.ProcMatch{Tool: "Claude Code", Candidate: true}, true
	case lspNames[n]:
		return engine.ProcMatch{Tool: "Language servers", Candidate: true}, true
	case isRuntime(n):
		switch {
		case claudeCodeRe.MatchString(cmd):
			return engine.ProcMatch{Tool: "Claude Code", Candidate: true}, true
		case mcpRe.MatchString(cmd):
			return engine.ProcMatch{Tool: "MCP servers", Candidate: true}, true
		case lspRe.MatchString(cmd):
			return engine.ProcMatch{Tool: "Language servers", Candidate: true}, true
		case devRuntimes[n] && devRe.MatchString(cmd):
			return engine.ProcMatch{Tool: "Dev servers", Candidate: true}, true
		case claudeDirRe.MatchString(cmd): // plugin workers living under ~/.claude
			return engine.ProcMatch{Tool: "Claude Code", Candidate: true}, true
		}
	}
	return engine.ProcMatch{}, false
}

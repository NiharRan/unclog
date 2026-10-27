package detectors

import (
	"testing"

	"github.com/NiharRan/unclog/internal/engine"
)

func TestMatchAIProcess(t *testing.T) {
	cases := []struct {
		name, cmd string
		tool      string // "" = must not match
		candidate bool
	}{
		// MCP servers
		{"node", "node /x/node_modules/@modelcontextprotocol/server-filesystem/dist/index.js /tmp", "MCP servers", true},
		{"node", "npm exec @modelcontextprotocol/server-github", "MCP servers", true},
		{"python3.12", "python3.12 -m mcp_server_fetch", "MCP servers", true},
		{"uv", "uv run mcp-server-git --repository .", "MCP servers", true},
		{"node", "node /x/node_modules/.bin/mcp-remote https://example.com/sse", "MCP servers", true},
		{"node", "node /x/@playwright/mcp/cli.js", "MCP servers", true},
		{"node", "node /Users/me/app/server.js", "", false},
		{"python3", "python3 -m mcpdf", "", false},
		{"node", "node ./scripts/campaign.js", "", false},
		// language servers
		{"typescript-language-server", "typescript-language-server --stdio", "Language servers", true},
		{"node", "node /x/typescript/lib/tsserver.js --useInferredProjectPerProjectRoot", "Language servers", true},
		{"gopls", "gopls serve", "Language servers", true},
		{"rust-analyzer", "rust-analyzer", "Language servers", true},
		{"node", "node /x/pyright/langserver.index.js --stdio", "Language servers", true},
		// dev servers
		{"node", "node /p/node_modules/.bin/vite", "Dev servers", true},
		{"node", "node /p/node_modules/vite/bin/vite.js --port 5173", "Dev servers", true},
		{"node", "node /p/node_modules/next/dist/bin/next dev", "Dev servers", true},
		{"next-server (v14.2.3)", "next-server (v14.2.3)", "", false}, // not a known process name; the node form is matched
		{"node", "node /p/node_modules/.bin/webpack serve", "Dev servers", true},
		{"node", "node /Users/me/vite-project/scripts/build.js", "", false},
		{"node", "node /p/node_modules/.bin/webpack --mode production", "", false},
		{"python3", "python3 -m http.server vite", "", false}, // dev servers are node-family only
		// Claude Code
		{"claude", "claude", "Claude Code", true},
		{"2.1.311", "claude", "Claude Code", true},
		{"2.1.311", "/usr/local/bin/claude --resume", "Claude Code", true},
		{"2.1.311", "/usr/local/bin/other-tool", "", false},
		{"node", "node /x/node_modules/@anthropic-ai/claude-code/cli.js", "Claude Code", true},
		{"Python", "/opt/py/Python /Users/me/.claude/plugins/data/x/dashboard-server.py", "Claude Code", true},
		{"python3", "python3 /Users/me/work/app.py", "", false},
		// apps and services (never orphan candidates unless helpers)
		{"Cursor", "/Applications/Cursor.app/Contents/MacOS/Cursor", "Cursor", false},
		{"Cursor Helper (Renderer)", "/Applications/Cursor.app/.../Cursor Helper (Renderer)", "Cursor", true},
		{"Code Helper (Plugin)", "Code Helper (Plugin)", "VS Code", true},
		{"Code", "/Applications/Visual Studio Code.app/Contents/MacOS/Electron", "VS Code", false},
		{"code", "/usr/share/code/code --type=renderer", "VS Code", true},
		{"Cursor.exe", `C:\Users\u\AppData\Local\Programs\cursor\Cursor.exe --type=gpu-process`, "Cursor", true},
		{"CursorUIViewService", "/System/Library/.../CursorUIViewService", "", false},
		{"ollama", "ollama serve", "Ollama", false},
		{"Windsurf Helper", "Windsurf Helper", "Windsurf", true},
		{"zsh", "/bin/zsh -il", "", false},
	}
	for _, c := range cases {
		t.Run(c.name+"/"+c.cmd, func(t *testing.T) {
			m, ok := MatchAIProcess(engine.Proc{Name: c.name, Cmdline: c.cmd})
			if (c.tool != "") != ok {
				t.Fatalf("matched = %v (%+v); want tool %q", ok, m, c.tool)
			}
			if ok && (m.Tool != c.tool || m.Candidate != c.candidate) {
				t.Errorf("got %+v; want tool=%q candidate=%v", m, c.tool, c.candidate)
			}
		})
	}
}

func TestInteresting(t *testing.T) {
	for name, want := range map[string]bool{
		"node": true, "Python": true, "python3.12": true, "uv": true, "claude": true, "2.1.311": true,
		"Cursor Helper (GPU)": true, "Code": true, "ollama": true, "gopls": true, "node.exe": true,
		"zsh": false, "Finder": false, "CursorUIViewService": false, "": false,
	} {
		if got := Interesting(name); got != want {
			t.Errorf("Interesting(%q) = %v; want %v", name, got, want)
		}
	}
}

// Package agent names the AI coding agent, if any, whose process is calling
// the API, for the User-Agent header. The agent names and variables are the
// ones runpodctl and runpod-python use, which follow Hugging Face's
// agent-harnesses list
// (https://github.com/huggingface/huggingface.js/blob/main/packages/tasks/src/agent-harnesses.ts),
// so every Runpod client reports an agent under the same name.
package agent

import (
	"os"
	"strings"
)

// registry is checked top to bottom and the first variable set to a non-empty
// value wins, so a narrower signal sits above a broader one it can appear with:
// cowork above claude-code, cursor-cli above cursor.
var registry = []struct {
	name string
	vars []string
}{
	{"antigravity", []string{"ANTIGRAVITY_AGENT"}},
	{"augment-cli", []string{"AUGMENT_AGENT"}},
	{"cline", []string{"CLINE_ACTIVE"}},
	{"cowork", []string{"CLAUDE_CODE_IS_COWORK"}},
	{"claude-code", []string{"CLAUDECODE", "CLAUDE_CODE"}},
	{"codex", []string{"CODEX_SANDBOX", "CODEX_CI", "CODEX_THREAD_ID"}},
	{"crush", []string{"CRUSH"}},
	{"gemini-cli", []string{"GEMINI_CLI"}},
	{"github-copilot", []string{"COPILOT_MODEL", "COPILOT_ALLOW_ALL", "COPILOT_GITHUB_TOKEN"}},
	{"goose", []string{"GOOSE_TERMINAL"}},
	{"hermes-agent", []string{"HERMES_SESSION_ID"}},
	{"kilo-code", []string{"KILOCODE_FEATURE"}},
	{"kiro", []string{"AGENT_CONTEXT_OUT"}},
	{"openclaw", []string{"OPENCLAW_SHELL"}},
	{"opencode", []string{"OPENCODE_CLIENT"}},
	{"pi", []string{"PI_CODING_AGENT"}},
	{"replit", []string{"REPL_ID"}},
	{"trae", []string{"TRAE_AI_SHELL_ID"}},
	{"zed", []string{"ZED_TERM"}},
	{"cursor-cli", []string{"CURSOR_AGENT"}},
	{"cursor", []string{"CURSOR_TRACE_ID"}},
}

// fallbackVar lets any other tool name itself; its value goes through Token.
// A bare AGENT is not read: CI runners and shell setups set it for unrelated
// reasons.
const fallbackVar = "AI_AGENT"

// Name returns the detected agent's name, or "" when none is detected.
func Name() string {
	for _, entry := range registry {
		for _, v := range entry.vars {
			if os.Getenv(v) != "" {
				return entry.name
			}
		}
	}
	return Token(os.Getenv(fallbackVar))
}

// EnvVars lists every variable Name reads, for tests that must clear them.
func EnvVars() []string {
	vars := []string{fallbackVar}
	for _, entry := range registry {
		vars = append(vars, entry.vars...)
	}
	return vars
}

// Token trims s and keeps only the characters safe in a User-Agent token
// (A-Z, a-z, 0-9, '.', '_', '-'), at most 64 of them, so a value taken from
// the environment cannot break the header.
func Token(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if b.Len() == 64 {
			break
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

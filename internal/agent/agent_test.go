package agent

import (
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, v := range EnvVars() {
		t.Setenv(v, "")
	}
}

func TestName(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"none", nil, ""},
		{"claude-code", map[string]string{"CLAUDECODE": "1"}, "claude-code"},
		{"claude-code alternate variable", map[string]string{"CLAUDE_CODE": "1"}, "claude-code"},
		{"codex", map[string]string{"CODEX_THREAD_ID": "abc"}, "codex"},
		{"cowork above claude-code", map[string]string{"CLAUDECODE": "1", "CLAUDE_CODE_IS_COWORK": "1"}, "cowork"},
		{"cursor-cli above cursor", map[string]string{"CURSOR_TRACE_ID": "abc", "CURSOR_AGENT": "1"}, "cursor-cli"},
		{"fallback", map[string]string{"AI_AGENT": "my-harness"}, "my-harness"},
		{"registry above fallback", map[string]string{"AI_AGENT": "my-harness", "CLAUDECODE": "1"}, "claude-code"},
		{"fallback cleaned", map[string]string{"AI_AGENT": " my harness\r\nX-Injected: 1 "}, "myharnessX-Injected1"},
		{"fallback with nothing usable", map[string]string{"AI_AGENT": "!@#$%"}, ""},
		{"bare AGENT ignored", map[string]string{"AGENT": "ci-runner"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("AGENT", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if got := Name(); got != tt.want {
				t.Errorf("Name() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTokenCapsLength(t *testing.T) {
	if got := Token(strings.Repeat("a", 100)); len(got) != 64 {
		t.Errorf("len(Token) = %d, want 64", len(got))
	}
}

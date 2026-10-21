package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComposeAppendsAfterTheDraft(t *testing.T) {
	ctx := "# ─── Claude's last response (for reference; removed on save) ───\n# ответ\n# ─── Write your reply below this line ──────\n\n"
	for _, c := range []struct{ content, want string }{
		{"", "P\n"},
		{"\n\n", "P\n"},
		{"мой черновик", "мой черновик\n\nP\n"},
		{"мой черновик\n\n", "мой черновик\n\nP\n"},
		{ctx, ctx[:len(ctx)-2] + "\n\nP\n"}, // Claude Code drops the # block on save
		{ctx + "черновик", ctx + "черновик\n\nP\n"},
	} {
		if got := compose(c.content, "P"); got != c.want {
			t.Errorf("compose(%q) = %q, want %q", c.content, got, c.want)
		}
	}
}

func TestWritePrompt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "claude-prompt-1.md")
	if err := os.WriteFile(file, []byte("черновик"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePrompt(file, "P"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); string(got) != "черновик\n\nP\n" {
		t.Fatalf("file %q", got)
	}
}

func TestOnlyClaudesFileIsReviewed(t *testing.T) {
	for name, want := range map[string]bool{
		"claude-prompt-1d7d5efe-98b3-447e-a9e9-0d2a976e8835.md": true,
		"COMMIT_EDITMSG": false, "git-rebase-todo": false, "claude-prompt.txt": false,
	} {
		if got := claudePrompt.MatchString(name); got != want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

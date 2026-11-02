package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCodexReplies(t *testing.T) {
	for _, tc := range []struct{ name, rows string }{
		{"legacy", `
{"type":"session_meta","payload":{"id":"test"}}
{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"injected instructions"}]}}
{"type":"event_msg","payload":{"type":"user_message","message":"question"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"working","phase":"commentary"}}
{"type":"response_item","payload":{"role":"assistant","content":[{"type":"output_text","text":"answer"}]}}
{"type":"event_msg","payload":{"type":"agent_message","message":"answer"}}
{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":"answer"}}
{"type":"compacted","payload":{"message":"summary"}}
{"type":"event_msg","payload":{"type":"user_message","message":"next"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"unfinished","phase":"commentary"}}
{"type":"event_msg","payload":{"type":"turn_aborted"}}
`},
		{"paginated", `
{"type":"session_meta","payload":{"id":"test"}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":"question"}]}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","phase":"commentary","content":[{"type":"Text","text":"working"}]}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"Reasoning","content":[{"type":"Text","text":"private"}]}}}
{"type":"response_item","payload":{"role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"answer"}]}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","phase":"final_answer","content":[{"type":"Text","text":"answer"}]}}}
{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":"answer"}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":"next"}]}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","phase":"commentary","content":[{"type":"Text","text":"unfinished"}]}}}
{"type":"event_msg","payload":{"type":"turn_aborted"}}
{"type":"event_msg","payload":
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readCodexReplies(strings.NewReader(tc.rows))
			if err != nil || !reflect.DeepEqual(got, []Reply{{Text: "answer", Prompt: "question"}}) {
				t.Fatalf("got %#v, %v", got, err)
			}
		})
	}
}

func TestCodexRollbackCountsAbortedTurns(t *testing.T) {
	rows := `{"type":"event_msg","payload":{"type":"user_message","message":"question"}}
{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":"keep"}}
{"type":"event_msg","payload":{"type":"user_message","message":"cancelled"}}
{"type":"event_msg","payload":{"type":"turn_aborted"}}
{"type":"event_msg","payload":{"type":"thread_rolled_back","num_turns":1}}`
	got, err := readCodexReplies(strings.NewReader(rows))
	if err != nil || !reflect.DeepEqual(got, []Reply{{Text: "keep", Prompt: "question"}}) {
		t.Fatalf("%#v, %v", got, err)
	}
}

func TestTranscriptFormatDetection(t *testing.T) {
	for _, rows := range []string{
		`{"type":"session_meta","payload":{"id":"test"}}
{"type":"event_msg","payload":{"type":"user_message","message":"question"}}
{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":"answer"}}`,
		`{"type":"user","message":{"content":"question"}}
{"type":"assistant","message":{"id":"a","content":[{"type":"text","text":"answer"}]}}`,
	} {
		got, err := readSessionReplies(strings.NewReader(rows))
		if err != nil || !reflect.DeepEqual(got, []Reply{{Text: "answer", Prompt: "question"}}) {
			t.Fatalf("%#v, %v", got, err)
		}
	}
}

func TestSessionPickerDoesNotGuessFromInheritedThread(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "other")
	m := newSessionPicker([]codexSession{{path: "other.jsonl", id: "other", title: "other"}, {path: "wanted.jsonl", id: "wanted", title: "wanted"}})
	if m.chosen != "" {
		t.Fatal("must wait for selection")
	}
	next, _ := m.Update(tea.PasteMsg{Content: "wanted"})
	m = next.(sessionPicker)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(sessionPicker)
	if m.chosen != "wanted.jsonl" || !m.done {
		t.Fatalf("choice: %q", m.chosen)
	}
	m = newSessionPicker([]codexSession{{path: "other.jsonl"}})
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(sessionPicker)
	if m.chosen != "" || !m.done {
		t.Fatal("cancel must leave the prompt unchanged")
	}
}

func TestCodexRollbackAndFinalWithoutNewline(t *testing.T) {
	rows := `{"type":"event_msg","payload":{"type":"user_message","message":"old"}}
{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":"removed"}}
{"type":"event_msg","payload":{"type":"thread_rolled_back","num_turns":1}}
{"type":"event_msg","payload":{"type":"user_message","message":"new"}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","phase":"final_answer","content":[{"type":"Text","text":"kept"},{"type":"Text","text":"second block"}]}}}`
	got, err := readCodexReplies(strings.NewReader(rows))
	if err != nil || !reflect.DeepEqual(got, []Reply{{Text: "kept\n\nsecond block", Prompt: "new"}}) {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestCodexPromptRouting(t *testing.T) {
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "custom-codex"))
	for file, want := range map[string]bool{
		filepath.Join(codexHome(), "editor", ".tmpABC.md"): true,
		"/project/.codex/editor/.tmpABC.md":                true,
		"/project/.codex/editor/notes.md":                  false,
		"/project/editor/.tmpABC.md":                       false,
		"/tmp/.tmpABC.md":                                  false,
		"COMMIT_EDITMSG":                                   false,
	} {
		if got := isCodexPrompt(file); got != want {
			t.Errorf("%s: got %v, want %v", file, got, want)
		}
	}
}

func TestCodexSessionsUseMetadataAndTitles(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "11", "02")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"rollout-a.jsonl":     `{"type":"session_meta","payload":{"id":"a","cwd":"/first","source":"cli"}}`,
		"rollout-b.jsonl":     `{"type":"session_meta","payload":{"id":"b","cwd":"/second","source":"vscode"}}`,
		"rollout-child.jsonl": `{"type":"session_meta","payload":{"id":"child","cwd":"/first","source":{"subagent":{"thread_spawn":{}}}}}`,
		"broken.jsonl":        `{`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte("{\"id\":\"a\",\"thread_name\":\"old name\"}\n{\"id\":\"a\",\"thread_name\":\"Review tests\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessions, err := codexSessions(home)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("%#v, %v", sessions, err)
	}
	for _, s := range sessions {
		if s.id == "a" && (s.title != "Review tests" || s.cwd != "/first") {
			t.Fatalf("%#v", s)
		}
	}
}

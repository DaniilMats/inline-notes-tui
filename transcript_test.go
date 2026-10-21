package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRepliesAreTurns(t *testing.T) {
	rows := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"вопрос про \"assistant\""}}`,
		`{"type":"assistant","message":{"id":"m1","model":"claude","content":[{"type":"thinking","thinking":"..."}]}}`,
		`{"type":"assistant","message":{"id":"m1","model":"claude","content":[{"type":"text","text":"смотрю файлы"}]}}`,
		`{"type":"assistant","message":{"id":"m1","model":"claude","content":[{"type":"tool_use","name":"Bash"}]}}`,
		`{"type":"user","toolUseResult":{},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"id":"s1","model":"claude","content":[{"type":"text","text":"субагент"}]}}`,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"тело навыка"}]}}`,
		`{"type":"assistant","message":{"id":"m2","model":"claude","content":[{"type":"text","text":"## Итог\n"},{"type":"text","text":"ещё"}]}}`,
		`{"type":"assistant","message":{"id":"m2","model":"claude","content":[{"type":"tool_use","name":"Read"}]}}`,
		`{"type":"assistant","message":{"id":"x1","model":"<synthetic>","content":[{"type":"text","text":"API Error"}]}}`,
		`not json "assistant"`,
		`{"type":"user","message":{"role":"user","content":"<command-name>/reload-plugins</command-name>\n<command-message>reload</command-message>"}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"image","source":{}},{"type":"text","text":"[Image #1] что тут?"}]}}`,
		`{"type":"assistant","message":{"id":"m3","model":"claude","content":[{"type":"text","text":"  "}]}}`,
		`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"This session is being continued…"}}`,
		`{"type":"assistant","message":{"id":"m4","model":"claude","content":[{"type":"text","text":"продолжаю"}]}}`,
	}, "\n") // no newline after the last row
	got, err := readReplies(strings.NewReader(rows))
	if err != nil {
		t.Fatal(err)
	}
	want := []Reply{
		{Text: "## Итог\n\n\nещё", Prompt: `вопрос про "assistant"`}, // the turn's last text, not the note before the tool call
		{Text: "продолжаю", Prompt: "(продолжение после сжатия контекста)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("replies %q", got)
	}
	got, _ = readReplies(strings.NewReader(`{"type":"user","message":{"content":"<command-name>/fb</command-name>"}}` + "\n" +
		`{"type":"assistant","message":{"id":"a","model":"c","content":[{"type":"text","text":"ок"}]}}`))
	if len(got) != 1 || got[0].Prompt != "/fb" {
		t.Fatalf("a slash command reads as its name: %q", got)
	}
}

func TestFindTranscript(t *testing.T) {
	config := t.TempDir()
	cwd := "/Users/x/my.project"
	dir := filepath.Join(config, "projects", "-Users-x-my-project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		_ = os.Chtimes(p, at, at)
		return p
	}
	older := write("aaa.jsonl", time.Hour)
	newest := write("bbb.jsonl", time.Minute)

	// No registry for the pid: the newest transcript of the working directory.
	if got, err := findTranscript(config, 4242, cwd); err != nil || got != newest {
		t.Fatalf("fallback: %q %v", got, err)
	}
	// The parent session's registry names its transcript, newest or not.
	if err := os.MkdirAll(filepath.Join(config, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := `{"pid":4242,"sessionId":"aaa","cwd":"/Users/x/my.project"}`
	if err := os.WriteFile(filepath.Join(config, "sessions", "4242.json"), []byte(reg), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := findTranscript(config, 4242, "/elsewhere"); err != nil || got != older {
		t.Fatalf("registry: %q %v", got, err)
	}
	// After /clear the registry names a session with no transcript yet: not an older one's.
	reg = `{"pid":4242,"sessionId":"ccc","cwd":"/Users/x/my.project"}`
	if err := os.WriteFile(filepath.Join(config, "sessions", "4242.json"), []byte(reg), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := findTranscript(config, 4242, cwd); got != filepath.Join(dir, "ccc.jsonl") {
		t.Fatalf("a fresh session: %q", got)
	}
	if _, err := findTranscript(config, 1, "/nowhere"); err == nil {
		t.Fatal("no transcripts is an error")
	}
}

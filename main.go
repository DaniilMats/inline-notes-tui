// inline-notes-tui reviews a Claude Code reply line by line, like a pull request, and hands
// the comments back as one prompt.
//
// Set as Claude Code's prompt editor (`"env": {"VISUAL": "inline-notes-tui"}` in settings.json),
// Ctrl+G opens it over the session's last reply; `s` writes the prompt after whatever was in
// the prompt box, and Claude Code puts it back there. Any other file it is asked to edit (a git
// commit message, say) goes to a plain editor: $FB_EDITOR, $EDITOR, else vi.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
)

// Claude Code's Ctrl+G hands its editor /tmp/claude-<uid>/claude-prompt-<uuid>.md.
var claudePrompt = regexp.MustCompile(`^claude-prompt-.*\.md$`)

func main() {
	n := flag.Int("n", 0, "номер ответа Claude (1 — первый); по умолчанию последний")
	transcript := flag.String("transcript", "", "транскрипт сессии (.jsonl); по умолчанию сессия, из которой открыт Ctrl+G")
	flag.Usage = func() {
		_, _ = fmt.Fprintln(flag.CommandLine.Output(), "usage: inline-notes-tui [-n N] [-transcript FILE] [PROMPT_FILE]\nБез PROMPT_FILE промпт печатается в stdout.")
		flag.PrintDefaults()
	}
	flag.Parse()
	file := flag.Arg(0)
	if file != "" && !claudePrompt.MatchString(filepath.Base(file)) {
		plainEditor(file)
	}
	if err := run(file, *transcript, *n); err != nil {
		fmt.Fprintln(os.Stderr, "inline-notes-tui:", err)
		os.Exit(1)
	}
}

func run(file, transcript string, n int) error {
	if transcript == "" {
		cwd, _ := os.Getwd()
		found, err := findTranscript(configDir(), os.Getppid(), cwd)
		if err != nil {
			return noReplies(file, err)
		}
		transcript = found
	}
	f, err := os.Open(transcript)
	if err != nil {
		return noReplies(file, err)
	}
	replies, err := readReplies(f)
	_ = f.Close() // read-only
	if err != nil {
		return noReplies(file, err)
	}
	if len(replies) == 0 {
		return noReplies(file, errors.New("в сессии ещё нет ответов Claude"))
	}
	pick := len(replies) - 1
	if n > 0 {
		pick = min(n, len(replies)) - 1
	}

	final, err := tea.NewProgram(newModel(replies, pick, file)).Run()
	if err != nil {
		return err
	}
	m := final.(model)
	switch m.outcome {
	case outSubmit:
		if file == "" {
			fmt.Println(m.r.buildPrompt())
			return nil
		}
		return writePrompt(file, m.r.buildPrompt())
	case outEdited:
		return m.err
	}
	return nil // cancelled: the file stays as it was, and so does the prompt box
}

// noReplies: with nothing to review, Ctrl+G still edits the prompt, in a plain editor.
func noReplies(file string, why error) error {
	if file == "" {
		return why
	}
	fmt.Fprintf(os.Stderr, "inline-notes-tui: %v; открываю обычный редактор\n", why)
	plainEditor(file)
	return nil
}

// writePrompt puts the prompt after the draft in Claude Code's file. Above the draft the file
// may hold Claude's last reply as `#` lines down to a `# ─── Write your reply below this line`
// rule; Claude Code drops those on save, so they stay as they are.
func writePrompt(file, prompt string) error {
	content, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	return os.WriteFile(file, []byte(compose(string(content), prompt)), 0o600)
}

func compose(content, prompt string) string {
	draft := strings.TrimRight(content, " \t\r\n")
	if draft == "" {
		return prompt + "\n"
	}
	return draft + "\n\n" + prompt + "\n"
}

// plainEditor replaces this process with a plain editor on file; its exit code is ours.
func plainEditor(file string) {
	argv := editorArgv()
	path, err := exec.LookPath(argv[0])
	if err == nil {
		err = syscall.Exec(path, append(argv, file), os.Environ())
	}
	fmt.Fprintln(os.Stderr, "inline-notes-tui: не удалось открыть редактор:", err)
	os.Exit(1)
}

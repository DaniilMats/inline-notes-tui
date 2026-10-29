package main

// Where the reply comes from: the session transcript Claude Code writes as it goes.
// Run from Ctrl+G, the parent process is the session's `claude`, which registers itself in
// <config>/sessions/<pid>.json with its session id and cwd. That file is Claude Code's own
// and undocumented, so the newest transcript of the working directory backs it up.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func configDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9]`)

// projectDir is where Claude Code keeps the transcripts of sessions run in cwd.
func projectDir(config, cwd string) string {
	return filepath.Join(config, "projects", slugRe.ReplaceAllString(cwd, "-"))
}

// findTranscript names the transcript of the session `pid` belongs to, else the newest one of cwd.
// The registry follows /clear and /resume; a session it names that has no transcript yet has no
// replies, rather than an older session's.
func findTranscript(config string, pid int, cwd string) (string, error) {
	var reg struct {
		SessionID string `json:"sessionId"`
		Cwd       string `json:"cwd"`
	}
	if data, err := os.ReadFile(filepath.Join(config, "sessions", fmt.Sprintf("%d.json", pid))); err == nil &&
		json.Unmarshal(data, &reg) == nil && reg.SessionID != "" && reg.Cwd != "" {
		return filepath.Join(projectDir(config, reg.Cwd), reg.SessionID+".jsonl"), nil
	}
	dir := projectDir(config, cwd)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf(tr("нет транскриптов сессий для %s: %w"), cwd, err)
	}
	newest, at := "", int64(0)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().UnixNano() > at {
			newest, at = filepath.Join(dir, e.Name()), info.ModTime().UnixNano()
		}
	}
	if newest == "" {
		return "", fmt.Errorf(tr("нет транскриптов сессий в %s"), dir)
	}
	return newest, nil
}

type transcriptRow struct {
	Type             string `json:"type"`
	IsSidechain      bool   `json:"isSidechain"`
	IsMeta           bool   `json:"isMeta"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	Message          struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Reply is a turn's answer: the last text Claude wrote before the person spoke again.
type Reply struct {
	Text   string
	Prompt string // what the person asked, as the list of replies shows it
}

var commandName = regexp.MustCompile(`^<command-name>([^<]*)</command-name>`)

// promptOf is what a user row asked, and whether it is the person's at all: a tool's result
// comes back in a user row too.
func promptOf(row transcriptRow) (string, bool) {
	if row.IsCompactSummary {
		return tr("(продолжение после сжатия контекста)"), true
	}
	var text string
	if json.Unmarshal(row.Message.Content, &text) == nil {
		if m := commandName.FindStringSubmatch(text); m != nil {
			return m[1], true // a slash command
		}
		return text, true
	}
	var blocks []contentBlock
	if json.Unmarshal(row.Message.Content, &blocks) != nil {
		return "", false
	}
	var parts []string
	asked := false
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts, asked = append(parts, b.Text), true
		case "image":
			asked = true
		}
	}
	return strings.Join(parts, "\n"), asked
}

// readReplies returns the session's replies, a turn each, in order. A turn runs from one of
// the person's prompts to the next; its reply is the last message in it with text, so the short
// notes Claude writes between tool calls are not replies. Claude Code writes a message a content
// block per row, all under the message's id. Subagents' rows and synthetic messages are skipped.
func readReplies(r io.Reader) ([]Reply, error) {
	var replies []Reply
	texts := map[string][]string{}
	prompt, last := "", ""
	flush := func() {
		if last != "" {
			replies = append(replies, Reply{Text: strings.TrimSpace(strings.Join(texts[last], "\n\n")), Prompt: prompt})
		}
		texts, last = map[string][]string{}, ""
	}
	in := bufio.NewReader(r)
	for {
		line, err := in.ReadBytes('\n')
		var row transcriptRow
		// Skip unmarshalling the rows that are neither: most of the file by size.
		if (bytes.Contains(line, []byte(`"assistant"`)) || bytes.Contains(line, []byte(`"user"`))) &&
			json.Unmarshal(line, &row) == nil && !row.IsSidechain {
			switch {
			case row.Type == "user" && !row.IsMeta:
				if p, ok := promptOf(row); ok {
					flush()
					prompt = p
				}
			case row.Type == "assistant" && row.Message.Model != "<synthetic>":
				var blocks []contentBlock
				if json.Unmarshal(row.Message.Content, &blocks) == nil {
					for _, b := range blocks {
						if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
							texts[row.Message.ID] = append(texts[row.Message.ID], b.Text)
							last = row.Message.ID
						}
					}
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	flush()
	return replies, nil
}

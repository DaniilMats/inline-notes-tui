package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func codexHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// Codex 0.166 writes .tmp*.md in CODEX_HOME/editor, ~/.codex/editor or <project>/.codex/editor.
func isCodexPrompt(file string) bool {
	dir, name := filepath.Dir(file), filepath.Base(file)
	if filepath.Base(dir) != "editor" || !strings.HasPrefix(name, ".tmp") || !strings.HasSuffix(name, ".md") {
		return false
	}
	return filepath.Clean(filepath.Dir(dir)) == filepath.Clean(codexHome()) || filepath.Base(filepath.Dir(dir)) == ".codex"
}

func readSessionReplies(r io.Reader) ([]Reply, error) {
	in := bufio.NewReader(r)
	first, err := in.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var header struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(first, &header)
	all := io.MultiReader(bytes.NewReader(first), in)
	if header.Type == "session_meta" {
		return readCodexReplies(all)
	}
	return readReplies(all)
}

// UI events exclude injected instructions and reasoning, and avoid response_item duplicates.
// Old rollouts use user_message/agent_message; paginated history uses item_completed.
func readCodexReplies(r io.Reader) ([]Reply, error) {
	var replies []Reply
	prompt, answer := "", ""
	active := false
	flush := func() {
		if active {
			replies = append(replies, Reply{Text: strings.TrimSpace(answer), Prompt: prompt})
		}
		answer = ""
		active = false
	}
	in := bufio.NewReader(r)
	for {
		line, err := in.ReadBytes('\n')
		var row struct {
			Type    string `json:"type"`
			Payload struct {
				Type     string `json:"type"`
				Message  string `json:"message"`
				Phase    string `json:"phase"`
				Last     string `json:"last_agent_message"`
				NumTurns int    `json:"num_turns"`
				Item     struct {
					Type    string         `json:"type"`
					Phase   string         `json:"phase"`
					Content []contentBlock `json:"content"`
				} `json:"item"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &row) == nil && row.Type == "event_msg" {
			p := row.Payload
			kind, text, phase := p.Type, p.Message, p.Phase
			if kind == "item_completed" {
				kind, phase = p.Item.Type, p.Item.Phase
				var parts []string
				for _, b := range p.Item.Content {
					if strings.EqualFold(b.Type, "text") {
						parts = append(parts, b.Text)
					}
				}
				text = strings.Join(parts, "\n\n")
			}
			switch kind {
			case "user_message", "UserMessage":
				if answer != "" {
					flush()
				}
				prompt = text
				active = true
			case "agent_message", "AgentMessage":
				if (phase == "" || phase == "final_answer" || phase == "final") && strings.TrimSpace(text) != "" {
					answer = text
				}
			case "task_complete":
				if p.Last != "" {
					answer = p.Last
				}
				flush()
			case "turn_aborted":
				answer = ""
				flush() // an empty aborted turn still counts when Codex rolls turns back
			case "thread_rolled_back":
				answer = ""
				replies = replies[:max(0, len(replies)-max(0, p.NumTurns))]
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
	var answered []Reply
	for _, reply := range replies {
		if reply.Text != "" {
			answered = append(answered, reply)
		}
	}
	return answered, nil
}

type codexSession struct {
	path, id, title, cwd string
	updated              time.Time
}

// The editor receives no session ID, even when CODEX_THREAD_ID is inherited from a different
// agent. Offer named local sessions instead of guessing from cwd or the newest transcript.
func codexSessions(home string) ([]codexSession, error) {
	titles := map[string]string{}
	if f, err := os.Open(filepath.Join(home, "session_index.jsonl")); err == nil {
		in := bufio.NewReader(f)
		for {
			line, err := in.ReadBytes('\n')
			var row struct {
				ID   string `json:"id"`
				Name string `json:"thread_name"`
			}
			if json.Unmarshal(line, &row) == nil {
				titles[row.ID] = row.Name
			}
			if err != nil {
				break
			}
		}
		_ = f.Close()
	}
	var sessions []codexSession
	for _, pattern := range []string{"sessions/*/*/*/*.jsonl", "archived_sessions/*.jsonl"} {
		paths, err := filepath.Glob(filepath.Join(home, pattern))
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			line, _ := bufio.NewReader(f).ReadBytes('\n')
			info, statErr := f.Stat()
			_ = f.Close()
			var row struct {
				Type    string `json:"type"`
				Payload struct {
					ID     string          `json:"id"`
					Cwd    string          `json:"cwd"`
					Source json.RawMessage `json:"source"`
				} `json:"payload"`
			}
			if statErr != nil || json.Unmarshal(line, &row) != nil || row.Type != "session_meta" || row.Payload.ID == "" {
				continue
			}
			var source string
			if json.Unmarshal(row.Payload.Source, &source) != nil {
				continue
			} // subagent source is an object
			title := titles[row.Payload.ID]
			if title == "" {
				title = row.Payload.ID
			}
			sessions = append(sessions, codexSession{path, row.Payload.ID, title, row.Payload.Cwd, info.ModTime()})
		}
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].updated.After(sessions[j].updated) })
	if len(sessions) == 0 {
		return nil, errors.New(tr("нет локальных сессий Codex"))
	}
	return sessions, nil
}

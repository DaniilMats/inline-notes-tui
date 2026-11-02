package main

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type sessionPicker struct {
	sessions           []codexSession
	filter             textinput.Model
	sel, width, height int
	chosen             string
	plain, done        bool
}

func newSessionPicker(sessions []codexSession) sessionPicker {
	f := textinput.New()
	f.Prompt = "/ "
	f.Placeholder = tr("Название, папка или ID сессии")
	f.Focus()
	return sessionPicker{sessions: sessions, filter: f}
}

func (m sessionPicker) Init() tea.Cmd { return textinput.Blink }

func (m sessionPicker) matches() []codexSession {
	var out []codexSession
	q := strings.ToLower(m.filter.Value())
	for _, s := range m.sessions {
		if strings.Contains(strings.ToLower(s.title+" "+s.cwd+" "+s.id), q) {
			out = append(out, s)
		}
	}
	return out
}

func (m sessionPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	items := m.matches()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.filter.SetWidth(max(1, msg.Width-4))
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.done = true
			return m, tea.Quit
		case "ctrl+e":
			m.plain, m.done = true, true
			return m, tea.Quit
		case "enter":
			if len(items) > 0 {
				m.chosen, m.done = items[m.sel].path, true
				return m, tea.Quit
			}
			return m, nil
		case "up", "down", "pgup", "pgdown":
			step := map[string]int{"up": -1, "down": 1, "pgup": -max(1, (m.height-5)/2), "pgdown": max(1, (m.height-5)/2)}[msg.String()]
			m.sel = max(0, min(len(items)-1, m.sel+step))
			return m, nil
		}
	}
	before := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != before {
		m.sel = 0
	}
	return m, cmd
}

func (m sessionPicker) View() tea.View {
	if m.done || m.width == 0 {
		return tea.NewView("")
	}
	w := max(1, m.width-2)
	lines := []string{tr("Codex · выберите сессию для комментариев"), tr("↑/↓ выбрать · Enter открыть · Esc отменить · ctrl+e редактор"), m.filter.View(), ""}
	items := m.matches()
	count := max(1, (m.height-5)/2)
	start := max(0, m.sel-count+1)
	for i := start; i < min(len(items), start+count); i++ {
		s := items[i]
		style, mark := lipgloss.NewStyle(), "  "
		if i == m.sel {
			style = style.Reverse(true)
			mark = "› "
		}
		title := strings.Join(strings.Fields(ansi.Strip(s.title)), " ")
		cwd := strings.Join(strings.Fields(ansi.Strip(s.cwd)), " ")
		lines = append(lines, style.Render(ansi.Truncate(mark+title, w, "…")), ansi.Truncate("  "+s.updated.Format("2006-01-02 15:04")+" · "+cwd, w, "…"))
	}
	if len(items) == 0 {
		lines = append(lines, tr("Сессии не найдены"))
	}
	for i, l := range lines {
		lines[i] = " " + ansi.Truncate(l, w, "")
	}
	v := tea.NewView(strings.Join(lines[:min(len(lines), max(1, m.height))], "\n"))
	v.AltScreen = true
	return v
}

func chooseCodexSession(home, file string) (string, error) {
	sessions, err := codexSessions(home)
	if err != nil {
		return "", err
	}
	if current := currentCodexSession(sessions, os.Getppid()); current != "" {
		return current, nil
	}
	final, err := tea.NewProgram(newSessionPicker(sessions)).Run()
	if err != nil {
		return "", err
	}
	m := final.(sessionPicker)
	if m.plain {
		plainEditor(file)
	}
	return m.chosen, nil
}

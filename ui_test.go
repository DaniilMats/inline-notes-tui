package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func press(m model, keys ...string) model {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "shift+enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "left":
			msg = tea.KeyPressMsg{Code: tea.KeyLeft}
		case "ctrl+c":
			msg = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
		default:
			r := []rune(k)
			msg = tea.KeyPressMsg{Code: r[0], Text: k}
		}
		next, _ := m.Update(msg)
		m = next.(model)
	}
	return m
}

func typed(m model, text string) model {
	for _, r := range text {
		next, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(model)
	}
	return m
}

func sized(texts []string, w, h int) model {
	replies := make([]Reply, len(texts))
	for i, t := range texts {
		replies[i] = Reply{Text: t, Prompt: "запрос " + t}
	}
	next, _ := newModel(replies, len(replies)-1, "").Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(model)
}

// rowOf finds the screen row (y) showing text.
func rowOf(t *testing.T, m model, text string) int {
	t.Helper()
	for y, l := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(ansi.Strip(l), text) {
			return y
		}
	}
	t.Fatalf("%q not on screen:\n%s", text, ansi.Strip(m.View().Content))
	return -1
}

func click(m model, y int) model {
	next, _ := m.Update(tea.MouseClickMsg{X: 20, Y: y, Button: tea.MouseLeft})
	return next.(model)
}

func TestClicksMoveCommentAndEdit(t *testing.T) {
	m := press(sized([]string{"старый ответ", reply}, 80, 20), "m") // the source: a click picks a line
	if m.r.Index != 2 || m.r.Text != reply {
		t.Fatal("the last reply is reviewed")
	}
	m = click(m, rowOf(t, m, "Поход в базу"))
	if m.r.Cursor != 1 || m.r.Draft != nil {
		t.Fatalf("a first click moves the cursor: %d", m.r.Cursor)
	}
	m = click(m, rowOf(t, m, "Поход в базу"))
	if m.r.Draft == nil || rowOf(t, m, "Новый комментарий к L2") < 0 {
		t.Fatal("a click on the cursor's line opens a comment")
	}
	m = typed(m, "абв")
	m = press(m, "left", "shift+enter")
	next, _ := m.Update(tea.PasteMsg{Content: "вставка"})
	m = press(next.(model), "enter")
	if len(m.r.Comments) != 1 || m.r.Comments[0].Body != "аб\nвставкав" {
		t.Fatalf("typed, a new line, a paste at the caret: %+v", m.r.Comments)
	}
	m = click(m, rowOf(t, m, "↳ #1 L2: аб"))
	if m.r.Draft == nil || m.r.Draft.ID != 1 || m.field.Value() != "аб\nвставкав" {
		t.Fatalf("a click on a comment edits it: %+v %q", m.r.Draft, m.field.Value())
	}
	m = press(m, "esc")
	if m.r.Draft != nil || m.r.Comments[0].Body != "аб\nвставкав" {
		t.Fatal("Esc drops the edit, the comment stays")
	}
	m = press(typed(press(m, "enter"), "x"), "ctrl+c")
	if m.r.Draft != nil || len(m.r.Comments) != 1 || m.outcome != "" {
		t.Fatal("ctrl+c in the field drops the comment, as Esc does, and leaves the review open")
	}
}

func TestKeysWhileTypingGoToTheComment(t *testing.T) {
	// The source, so the comment is on the first line alone.
	m := press(sized([]string{reply}, 80, 20), "m", "enter")
	m = typed(m, "jqs?[") // the review's keys, typed into the field
	if m.outcome != "" || m.r.Help || !strings.Contains(m.field.Value(), "jqs?[") {
		t.Fatalf("keys went to the review: %q %q", m.outcome, m.field.Value())
	}
	m = press(m, "enter")
	if m = press(m, "ы"); m.outcome != outSubmit {
		t.Fatal("s on the Russian layout submits")
	}
	if !strings.Contains(m.r.buildPrompt(), "> 1: Флаки есть, но их не видно.\nКомментарий:\njqs?[") {
		t.Fatalf("prompt %s", m.r.buildPrompt())
	}
}

func TestRenderedClicksLandOnBlocks(t *testing.T) {
	m := sized([]string{md}, 80, 40)
	if !m.r.Preview {
		t.Fatal("a reply opens rendered")
	}
	screen := ansi.Strip(m.View().Content)
	if strings.Contains(screen, "```") || strings.Contains(screen, "|---|") {
		t.Fatalf("the markdown is rendered:\n%s", screen)
	}
	m = click(m, rowOf(t, m, "хвост"))
	if m.r.Cursor != 13 {
		t.Fatalf("a click on a list item's second row picks the item: %d", m.r.Cursor)
	}
	m = click(m, rowOf(t, m, "x := 1"))
	if m.r.Cursor != 8 {
		t.Fatalf("a click in a code block picks the fence: %d", m.r.Cursor)
	}
	m = click(m, rowOf(t, m, "y := 2"))
	if m.r.Draft == nil || m.r.Draft.Start != 8 || m.r.Draft.End != 12 {
		t.Fatalf("a second click in the block comments on all of it: %+v", m.r.Draft)
	}
	m = press(typed(m, "ок"), "enter")
	if rowOf(t, m, "↳ #1 L9-L13: ок") <= rowOf(t, m, "y := 2") {
		t.Fatal("the comment sits under its block")
	}
}

func TestTablesKeepTheirWidthAndBlocksTheirRows(t *testing.T) {
	table := []string{"| Колонка | Значение |", "|---|---|", "| альфа | 1 |", "| `код` | **жирный** |"}
	if got := tableWidth(table); got != 20 { // 7+2 and 8+2: the headers are the widest cells
		t.Fatalf("tableWidth = %d: a column its widest cell and two spaces, a rule between", got)
	}
	if tableWidth([]string{"| a |", "текст"}) != 0 || tableWidth([]string{"| a |"}) != 0 {
		t.Fatal("only a whole table has a width")
	}
	m := sized([]string{"абзац\n\n" + strings.Join(table, "\n") + "\n\n1. первый шаг"}, 100, 30)
	head := strings.Split(ansi.Strip(m.View().Content), "\n")[rowOf(t, m, "Колонка")]
	if got := len([]rune(strings.TrimRight(head, " "))); got > padX+blockGutter+20 {
		t.Fatalf("the table is stretched to %d cells: %q", got, head)
	}
	if rowOf(t, m, "первый шаг")-rowOf(t, m, "жирный") != 2 {
		t.Fatal("a block takes its own rows and one blank row before the next, no more")
	}
	long := press(sized([]string{strings.Repeat("слово ", 40)}, 60, 20), "m")
	lines := strings.Split(ansi.Strip(long.View().Content), "\n")
	first := rowOf(t, long, "▌ 1 ")
	col := func(s string) int { return len([]rune(s[:strings.IndexRune(s, '│')])) }
	if col(lines[first]) != col(lines[first+1]) {
		t.Fatalf("a wrapped line's rows keep the gutter's rule in place:\n%s\n%s", lines[first], lines[first+1])
	}
}

func TestWindowFollowsTheCursorAndTheField(t *testing.T) {
	long := strings.Repeat("строка\n", 60) + "последняя"
	m := press(sized([]string{long}, 60, 15), "m")
	m = press(m, "G")
	rowOf(t, m, "последняя")
	m = press(m, "g")
	rowOf(t, m, "▌  1 ")
	m = press(m, "G", "enter")
	m = typed(m, strings.Repeat("длинный комментарий ", 12))
	y := rowOf(t, m, "Новый комментарий к L61")
	if c := m.View().Cursor; c == nil || c.Y <= y || c.Y >= m.height {
		t.Fatalf("the caret is in the field, on screen: %+v (box at %d)", c, y)
	}
}

func TestListOfReplies(t *testing.T) {
	texts := make([]string, 30)
	for i := range texts {
		texts[i] = fmt.Sprintf("## Ответ номер %d\nтекст", i+1)
	}
	m := press(sized(texts, 80, 20), "к") // r on the Russian layout
	if !m.picking || m.sel != 29 {
		t.Fatal("r opens the list on the reply under review")
	}
	rowOf(t, m, "› запрос ## Ответ номер 30") // what was asked
	rowOf(t, m, "Ответ номер 30 ")            // how the reply begins, its Markdown dropped
	if strings.Contains(ansi.Strip(m.View().Content), "Ответ номер 3\n") {
		t.Fatal("the window shows the end of a long list, where the cursor is")
	}
	m = press(m, "k", "k", "enter")
	if m.picking || m.r.Index != 28 || !strings.HasPrefix(m.r.Text, "## Ответ номер 28") {
		t.Fatalf("Enter opens the chosen reply: #%d", m.r.Index)
	}
	m = press(m, "r", "g")
	m = click(m, rowOf(t, m, "› запрос ## Ответ номер 2"))
	if m.picking || m.r.Index != 2 {
		t.Fatalf("a click opens a reply: #%d", m.r.Index)
	}
	m = press(m, "r", "j", "esc")
	if m.picking || m.r.Index != 2 {
		t.Fatal("Esc closes the list and keeps the reply")
	}
	m = press(typed(press(m, "c"), "x"), "enter", "r")
	if m.picking || !strings.Contains(m.r.Status, "сначала s") {
		t.Fatal("comments pin the review to its reply")
	}
}

func TestOtherReplies(t *testing.T) {
	m := sized([]string{"первый", "второй"}, 60, 15)
	m = press(m, "х")
	if m.r.Text != "первый" || m.r.Index != 1 {
		t.Fatal("[ reviews the reply before")
	}
	m = press(m, "c")
	m = press(typed(m, "x"), "enter", "]")
	if m.r.Text != "первый" || !strings.Contains(m.r.Status, "сначала s") {
		t.Fatal("comments pin the review to its reply")
	}
	if m = press(m, "q"); m.outcome != "" {
		t.Fatal("the first q with comments only arms")
	}
	if m = press(m, "q"); m.outcome != outCancel {
		t.Fatal("the second q cancels")
	}
}

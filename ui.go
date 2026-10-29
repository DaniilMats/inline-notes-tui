package main

// The reviewer on the whole terminal. Every row on screen is built here, so the rows a
// line, a rendered block or a comment take are known exactly: clicks map by them, and
// the window scrolls by them.

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	padX        = 1 // a cell of margin on each side of the screen
	blockGutter = 4 // rendered: the cursor mark, a space, the comment count, a space
)

// row is one terminal line of the body and what a click on it means.
type row struct {
	text string
	line int // the source line it shows, or its block's first line; -1 for none
	edit int // the comment it shows, 0 for none
}

type model struct {
	r       Review
	replies []Reply
	pick    int // replies[pick] is under review
	file    string
	picking bool // the list of replies is open
	sel     int  // the reply the list's cursor is on

	width, height int
	dark          bool
	field         textarea.Model
	md            map[string][]string           // rendered blocks by width and source, for the theme
	mdr           map[int]*glamour.TermRenderer // by the width they wrap at

	// The layout as last drawn: what View shows and what a click lands on.
	header []string
	rows   []row
	top    int
	box    int // the row the comment field's first line is drawn on, -1 without one
	boxX   int

	outcome string // outSubmit, outCancel, or outEdited after the editor was run
	err     error
}

const outEdited = "edited"

func newModel(replies []Reply, pick int, file string) model {
	f := textarea.New()
	f.Prompt = ""
	f.ShowLineNumbers = false
	f.DynamicHeight = true
	f.MinHeight = 1
	f.MaxHeight = 12
	f.MaxContentHeight = 5000
	f.Placeholder = tr("Enter — сохранить · ⇧Enter — новая строка · Esc — отмена")
	f.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"))
	f.SetVirtualCursor(false) // the terminal's own caret, where dictation and IME put text
	m := model{r: newReview(pick+1, replies[pick].Text), replies: replies, pick: pick, file: file, dark: true, field: f, box: -1}
	m.r.Preview = true // a reply is Markdown: it opens rendered, m shows the source
	m.r = m.r.setCursor(0)
	m.restyle()
	return m
}

func (m *model) restyle() {
	s := textarea.DefaultStyles(m.dark)
	s.Focused.CursorLine = lipgloss.NewStyle()
	s.Focused.Placeholder = lipgloss.NewStyle().Faint(true)
	m.field.SetStyles(s)
	m.md, m.mdr = map[string][]string{}, map[int]*glamour.TermRenderer{}
}

func (m model) Init() tea.Cmd { return tea.RequestBackgroundColor }

// Colors, after Claude Code's: its accent orange, a warm comment color, a blue selection.
func (m model) palette() (accent, comment, selection, band lipgloss.Style) {
	ld := lipgloss.LightDark(m.dark)
	accent = lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757"))
	comment = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#8A6200"), lipgloss.Color("#E5C07B")))
	selection = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#1F6FEB"), lipgloss.Color("#61AFEF")))
	band = lipgloss.NewStyle().Background(ld(lipgloss.Color("#E6E9F0"), lipgloss.Color("#30353F")))
	return
}

// inner is the columns between the margins.
func (m model) inner() int { return max(20, m.width-2*padX) }

// pad fills a styled line out to w cells, in style s.
func pad(text string, w int, s lipgloss.Style) string {
	if gap := w - ansi.StringWidth(text); gap > 0 {
		return text + s.Render(strings.Repeat(" ", gap))
	}
	return ansi.Truncate(text, w, "")
}

// wrap breaks plain text into rows of at most w cells: by words, a longer word across rows.
func wrap(text string, w int) []string {
	text = strings.ReplaceAll(text, "\t", "    ")
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, strings.Split(ansi.Wrap(line, max(1, w), ""), "\n")...)
	}
	return out
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.field.SetWidth(m.fieldWidth())
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.restyle()
	case tea.KeyPressMsg:
		if m.r.Draft != nil {
			cmd = m.typeKey(msg)
		} else {
			cmd = m.key(msg)
		}
	case tea.PasteMsg:
		if m.r.Draft != nil {
			m.field, cmd = m.field.Update(msg)
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			m.click(msg.Y)
		}
	case tea.MouseWheelMsg:
		if m.r.Draft == nil && !m.r.Help {
			step := map[tea.MouseButton]string{tea.MouseWheelUp: "up", tea.MouseWheelDown: "down"}[msg.Button]
			if m.picking {
				m.pickKey(step)
			} else if step != "" {
				m.r, _ = m.r.handleKey(step, 1)
			}
		}
	case editedMsg:
		m.err, m.outcome = msg.err, outEdited
		return m, tea.Quit
	default:
		if m.r.Draft != nil {
			m.field, cmd = m.field.Update(msg)
		}
	}
	if m.outcome != "" {
		return m, tea.Quit
	}
	m.layout()
	return m, cmd
}

// typeKey is a key while a comment is typed: Enter saves it, Esc drops it, the rest edit it.
func (m *model) typeKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.r = m.r.saveDraft(m.field.Value())
		m.field.Blur()
		return nil
	case "esc", "ctrl+c":
		m.r = m.r.cancelDraft()
		m.field.Blur()
		return nil
	}
	var cmd tea.Cmd
	m.field, cmd = m.field.Update(msg)
	return cmd
}

// open starts typing a comment, the field holding `body`.
func (m *model) open(r Review, body string) tea.Cmd {
	m.r = r
	if r.Draft == nil {
		return nil
	}
	m.field.Reset()
	m.field.SetWidth(m.fieldWidth())
	m.field.SetValue(body)
	return m.field.Focus()
}

func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	name := keyName(msg.String())
	switch {
	case m.r.Help:
		m.r.Help = false
		return nil
	case m.picking:
		m.pickKey(name)
		return nil
	case name == "r":
		m.openList()
		return nil
	case name == "e":
		return m.open(m.r.startEdit(0))
	case name == "c" || name == "enter":
		return m.open(m.r.startAdd(), "")
	case name == "[" || name == "]":
		m.switchReply(map[string]int{"[": -1, "]": 1}[name])
		return nil
	case name == "ctrl+e" && m.file != "":
		return m.runEditor()
	case name == "ctrl+c":
		name = "q"
	case name == "esc":
		if m.r.Anchor >= 0 {
			m.r = m.r.toggleSelection()
		}
		return nil
	}
	m.r, m.outcome = m.r.handleKey(name, max(1, m.height-len(m.header)-2))
	return nil
}

// Comments pin the review to its reply: another one opens once they are sent or deleted.
const pinned = "Комментарии относятся к этому ответу: сначала s или удалите их."

// switchReply reviews the reply before or after this one.
func (m *model) switchReply(by int) {
	next := m.pick + by
	switch {
	case len(m.r.Comments) > 0:
		m.r.Status = tr(pinned)
	case next < 0 || next >= len(m.replies):
		m.r.Status = tr("Других ответов в эту сторону нет.")
	default:
		m.load(next)
	}
}

// load puts reply i under review, in the same view.
func (m *model) load(i int) {
	preview := m.r.Preview
	m.pick, m.r = i, newReview(i+1, m.replies[i].Text)
	m.r.Preview = preview
	m.r = m.r.setCursor(0)
	m.top = 0
}

func (m *model) openList() {
	if len(m.r.Comments) > 0 {
		m.r.Status = tr(pinned)
		return
	}
	m.picking, m.sel, m.r.Status = true, m.pick, ""
}

// pickKey is a key in the list of replies: the arrows choose, Enter opens, Esc goes back.
func (m *model) pickKey(name string) {
	last, page := len(m.replies)-1, max(1, m.view()/2-1) // two rows a reply
	switch name {
	case "j", "down":
		m.sel = min(last, m.sel+1)
	case "k", "up":
		m.sel = max(0, m.sel-1)
	case "g", "home":
		m.sel = 0
	case "G", "end":
		m.sel = last
	case "ctrl+d", "pgdown", "space":
		m.sel = min(last, m.sel+page)
	case "ctrl+u", "pgup":
		m.sel = max(0, m.sel-page)
	case "enter":
		m.choose(m.sel)
	case "esc", "q", "r", "ctrl+c":
		m.picking = false
	}
}

func (m *model) choose(i int) {
	m.picking = false
	if i != m.pick {
		m.load(i)
	}
}

type editedMsg struct{ err error }

// runEditor hands the prompt file to a plain editor, the comments' prompt in it already.
func (m *model) runEditor() tea.Cmd {
	if len(m.r.Comments) > 0 {
		if err := writePrompt(m.file, m.r.buildPrompt()); err != nil {
			m.r.Status = tr("Не удалось записать промпт: %v", err)
			return nil
		}
	}
	argv := editorArgv()
	cmd := exec.Command(argv[0], append(argv[1:], m.file)...) //nolint:gosec // the person's own editor
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editedMsg{err} })
}

// click: on another line the cursor moves there, on the cursor's line a comment opens, on a comment it is edited.
func (m *model) click(y int) {
	i := m.top + y - len(m.header)
	if m.r.Draft != nil || m.r.Help || y < len(m.header) || i < 0 || i >= len(m.rows) || m.rows[i].line < 0 {
		return
	}
	hit := m.rows[i]
	if m.picking {
		m.choose(hit.line) // a reply's rows carry its index
		return
	}
	at := m.r.setCursor(hit.line)
	switch {
	case hit.edit != 0:
		m.open(at.startEdit(hit.edit))
	case at.Cursor == m.r.Cursor:
		m.open(at.startAdd(), "")
	default:
		m.r = at
	}
}

// Columns the source view's gutter takes: `▌ 12 3 │ `.
func (m model) gutter() int { return len(fmt.Sprint(len(m.r.Lines))) + 7 }

// text is the column the reply's text and the comments are drawn in: its left edge and width.
func (m model) text() (int, int) {
	g := m.gutter()
	if m.r.Preview {
		g = blockGutter
	}
	return g, max(10, m.inner()-g)
}

func (m model) fieldWidth() int {
	_, w := m.text()
	return max(8, w-4) // the box's borders and a space inside each
}

// layout builds the header and the body's rows and fits the window over them.
func (m *model) layout() {
	if m.width == 0 {
		return
	}
	accent, _, _, _ := m.palette()
	w := m.inner()
	m.header = nil
	if m.r.Help {
		m.header = []string{lipgloss.NewStyle().Bold(true).Render(tr("inline-notes · клавиши")) + tr(" (в скобках — русская раскладка; любая клавиша закрывает)"), ""}
		m.rows, m.top, m.box = m.helpRows(), 0, -1
		return
	}
	m.box = -1
	if m.picking {
		head := tr("inline-notes · ответы сессии: %d", len(m.replies))
		m.header = append(wrap(lipgloss.NewStyle().Bold(true).Render(head)+tr(" · по одному на ваш запрос"), w), wrap(m.hints(), w)...)
		m.header = append(m.header, accent.Faint(true).Render(strings.Repeat("─", w)))
		m.rows = m.listRows()
		m.fit()
		return
	}
	m.header = append(m.header, m.title()...)
	m.header = append(m.header, wrap(m.hints(), w)...)
	if m.r.Status != "" {
		for _, l := range wrap(m.r.Status, w) {
			m.header = append(m.header, lipgloss.NewStyle().Bold(true).Render(l))
		}
	}
	m.header = append(m.header, accent.Faint(true).Render(strings.Repeat("─", w)))

	if m.r.Preview {
		m.rows = m.renderedRows()
	} else {
		m.rows = m.sourceRows()
	}
	m.fit()
}

// snippet is a reply's first line with text, its Markdown marks dropped, in at most w cells.
func snippet(lines []string, w int) string {
	for _, l := range lines {
		if t := strings.Join(strings.Fields(strings.NewReplacer("`", "", "*", "", "_", "", "#", "", ">", "", "|", "").Replace(l)), " "); t != "" {
			return ansi.Truncate(t, w, "…")
		}
	}
	return ""
}

func (m model) title() []string {
	r := m.r
	parts := []string{tr("строка %d/%d", r.Cursor+1, len(r.Lines))}
	if r.Preview {
		b := blockIndex(r.Blocks, r.Cursor)
		parts = []string{tr("блок %d/%d (%s)", b+1, len(r.Blocks), lineRef(r.Blocks[b].Start, r.Blocks[b].End))}
	}
	if r.Anchor >= 0 {
		parts = append(parts, tr("выделение %s", lineRef(r.currentRange())))
	}
	parts = append(parts, tr("комментариев: %d", len(r.Comments)))
	head := tr("inline-notes · ответ #%d из %d «%s»", r.Index, len(m.replies), snippet(r.Lines, 40))
	return wrap(lipgloss.NewStyle().Bold(true).Render(head)+" · "+strings.Join(parts, " · "), m.inner())
}

func (m model) hints() string {
	pairs := [][2]string{{"j/k", tr("строка")}, {"v", tr("выделить")}, {"Enter", tr("коммент")}, {"e", tr("правка")}, {"m", tr("рендер")}, {"r", tr("ответы")}, {"s", tr("в промпт")}, {"q", tr("отмена")}, {"?", tr("все клавиши")}}
	switch {
	case m.picking:
		pairs = [][2]string{{"j/k", tr("выбрать")}, {"Enter", tr("открыть")}, {"Esc", tr("назад")}}
	case m.r.Draft != nil:
		pairs = [][2]string{{"Enter", tr("сохранить")}, {"⇧Enter", tr("новая строка")}, {"Esc", tr("отмена")}}
	case m.r.Preview:
		pairs[0][1], pairs[4][1] = tr("блок"), tr("исходник")
	}
	var out []string
	for _, p := range pairs {
		out = append(out, lipgloss.NewStyle().Bold(true).Render(p[0])+" "+p[1])
	}
	return strings.Join(out, " · ")
}

// commentRows are a saved comment's rows under its line or block.
func (m model) commentRows(c Comment, line int) []row {
	_, comment, _, _ := m.palette()
	left, w := m.text()
	var out []row
	for _, l := range wrap(fmt.Sprintf("↳ #%d %s: %s", c.ID, lineRef(c.Start, c.End), c.Body), w) {
		out = append(out, row{strings.Repeat(" ", left) + comment.Render(l), line, c.ID})
	}
	return out
}

// draftRows are the comment's box: a title in its top border, the field, the bottom border.
func (m *model) draftRows(line int, at int) []row {
	accent, _, _, _ := m.palette()
	left, w := m.text()
	d := m.r.Draft
	title := tr("Новый комментарий к %s", lineRef(d.Start, d.End))
	if d.ID != 0 {
		title = tr("Правка #%d к %s", d.ID, lineRef(d.Start, d.End))
	}
	indent := strings.Repeat(" ", left)
	top := "╭─ " + title + " "
	out := []row{{indent + accent.Render(top+strings.Repeat("─", max(0, w-ansi.StringWidth(top)-1))+"╮"), line, 0}}
	m.box, m.boxX = at+1, left+2
	for _, l := range strings.Split(m.field.View(), "\n") {
		out = append(out, row{indent + accent.Render("│") + " " + pad(l, w-4, lipgloss.NewStyle()) + " " + accent.Render("│"), line, 0})
	}
	return append(out, row{indent + accent.Render("╰"+strings.Repeat("─", max(0, w-2))+"╯"), line, 0})
}

func (m *model) sourceRows() []row {
	accent, comment, selection, band := m.palette()
	r := m.r
	w := m.inner()
	digits := len(fmt.Sprint(len(r.Lines)))
	_, cw := m.text()
	var out []row
	for i, line := range r.Lines {
		cur := i == r.Cursor
		count := " "
		if n := r.commentsOn(i, i); n > 9 {
			count = "*"
		} else if n > 0 {
			count = fmt.Sprint(n)
		}
		mark, num := " ", fmt.Sprintf("%*d", digits, i+1)
		base := lipgloss.NewStyle()
		if cur {
			base = band
			mark = "▌"
		}
		for k, l := range wrap(line, cw) {
			gutter := base.Render(strings.Repeat(" ", digits+5)) + base.Faint(true).Render("│ ")
			if k == 0 {
				numStyle := base.Faint(true)
				if cur {
					numStyle = base.Inherit(accent).Bold(true)
				}
				gutter = base.Inherit(accent).Render(mark) + base.Render(" ") + numStyle.Render(num) + base.Render(" ") +
					base.Inherit(comment).Render(count) + base.Faint(true).Render(" │ ")
			}
			textStyle := base.Bold(cur)
			if r.isSelected(i) {
				textStyle = textStyle.Inherit(selection).Reverse(true)
			}
			out = append(out, row{pad(gutter+textStyle.Render(l), w, base), i, 0})
		}
		for _, c := range r.Comments {
			if c.End == i && (r.Draft == nil || c.ID != r.Draft.ID) {
				out = append(out, m.commentRows(c, i)...)
			}
		}
		if r.Draft != nil && r.Draft.End == i {
			out = append(out, m.draftRows(i, len(out))...)
		}
	}
	return out
}

// listRows are the replies, oldest first, two rows each: what was asked, and how the reply begins.
func (m model) listRows() []row {
	accent, _, _, band := m.palette()
	w := m.inner()
	digits := len(fmt.Sprint(len(m.replies)))
	text := w - digits - 4 // the mark, `#`, the number, a space
	var out []row
	for i, rp := range m.replies {
		base, mark := lipgloss.NewStyle(), " "
		if i == m.sel {
			base, mark = band, "▌"
		}
		asked := strings.Join(strings.Fields(rp.Prompt), " ")
		if asked == "" {
			asked = tr("(без запроса)")
		}
		current := ""
		if i == m.pick {
			current = tr(" · открыт")
		}
		num := fmt.Sprintf("#%-*d ", digits, i+1)
		first := base.Inherit(accent).Render(mark) + base.Faint(true).Render(num) + base.Bold(true).Render(ansi.Truncate("› "+asked, text-ansi.StringWidth(current), "…")) + base.Faint(true).Render(current)
		second := base.Render(strings.Repeat(" ", digits+3)) + base.Faint(true).Render(snippet(strings.Split(rp.Text, "\n"), text))
		out = append(out, row{pad(first, w, base), i, 0}, row{pad(second, w, base), i, 0})
	}
	return out
}

// renderer is glamour wrapping at w, without the document's margin: the gutter is drawn here.
func (m *model) renderer(w int) *glamour.TermRenderer {
	if r, ok := m.mdr[w]; ok {
		return r
	}
	style := styles.DarkStyleConfig
	if !m.dark {
		style = styles.LightStyleConfig
	}
	zero := uint(0)
	style.Document.Margin = &zero
	style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""
	r, _ := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(w))
	m.mdr[w] = r
	return r
}

// markdown renders one block in w cells, as replies are drawn; a table at its own width,
// which glamour would stretch across all w.
func (m *model) markdown(lines []string, w int) []string {
	source := strings.Join(lines, "\n")
	k := fmt.Sprint(w, "\x00", source)
	if got, ok := m.md[k]; ok {
		return got
	}
	at := w
	if tw := tableWidth(lines); tw > 0 && tw < w {
		at = tw
	}
	out := wrap(source, w)
	if r := m.renderer(at); r != nil {
		if text, err := r.Render(source); err == nil {
			out = trimBlank(strings.Split(text, "\n"))
		}
	}
	for i := range out {
		out[i] = pad(out[i], w, lipgloss.NewStyle())
	}
	m.md[k] = out
	return out
}

var tableRule = regexp.MustCompile(`^\|?[\s:|-]+\|?$`)

// tableWidth is the width glamour draws a Markdown table at its natural size: each column its
// widest cell and a space on either side, a rule between columns; 0 for a block that is not a table.
func tableWidth(lines []string) int {
	if len(lines) < 2 {
		return 0
	}
	var cols []int
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "|") {
			return 0
		}
		if tableRule.MatchString(t) {
			continue
		}
		cells := strings.Split(strings.Trim(strings.ReplaceAll(t, `\|`, "¦"), "|"), "|")
		for i, c := range cells {
			w := ansi.StringWidth(strings.NewReplacer("**", "", "`", "", "¦", "|").Replace(strings.TrimSpace(c)))
			if strings.Contains(c, "`") {
				w += 2 // glamour pads inline code
			}
			if i >= len(cols) {
				cols = append(cols, 0)
			}
			cols[i] = max(cols[i], w)
		}
	}
	total := len(cols) - 1
	for _, c := range cols {
		total += c + 2
	}
	return total
}

// trimBlank drops the blank rows glamour puts around a block: the gaps between blocks are drawn here.
func trimBlank(lines []string) []string {
	blank := func(s string) bool { return strings.TrimSpace(ansi.Strip(s)) == "" }
	for len(lines) > 1 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 1 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (m *model) renderedRows() []row {
	accent, comment, selection, _ := m.palette()
	r := m.r
	_, w := m.text()
	from, to := -1, -1
	if r.Anchor >= 0 {
		from, to = r.currentRange()
	}
	cursor := blockIndex(r.Blocks, r.Cursor)
	var out []row
	for i, b := range r.Blocks {
		if i > 0 && b.Start > r.Blocks[i-1].End+1 {
			out = append(out, row{"", -1, 0})
		}
		mark := " "
		switch {
		case i == cursor:
			mark = accent.Render("▌")
		case from <= b.Start && b.End <= to:
			mark = selection.Render("┃")
		}
		count := " "
		if n := r.commentsOn(b.Start, b.End); n > 9 {
			count = "*"
		} else if n > 0 {
			count = fmt.Sprint(n)
		}
		for k, l := range m.markdown(r.Lines[b.Start:b.End+1], w) {
			gutter := mark + "   "
			if k == 0 {
				gutter = mark + " " + comment.Render(count) + " "
			}
			out = append(out, row{gutter + l, b.Start, 0})
		}
		for _, c := range r.Comments {
			if blockIndex(r.Blocks, c.End) == i && (r.Draft == nil || c.ID != r.Draft.ID) {
				out = append(out, m.commentRows(c, b.Start)...)
			}
		}
		if r.Draft != nil && blockIndex(r.Blocks, r.Draft.End) == i {
			out = append(out, m.draftRows(b.Start, len(out))...)
		}
	}
	return out
}

var helpKeys = [][2]string{
	{"j / k, ↑ / ↓ (о / л)", "строка вниз / вверх; в рендере — блок"},
	{"g / G, Home / End (п / П)", "в начало / в конец"},
	{"ctrl+d / ctrl+u, PgDn / PgUp, пробел", "страница вниз / вверх"},
	{"v (м)", "начать или снять выделение диапазона; Esc снимает"},
	{"Enter, c (с), клик по строке с курсором", "комментарий к строке или диапазону"},
	{"e (у), клик по комментарию", "править комментарий"},
	{"d (в)", "удалить комментарий на строке"},
	{"n / N (т / Т)", "следующий / предыдущий комментарий"},
	{"m (ь)", "исходник или отрисованный markdown (открывается он): в нём курсор и комментарии — по блокам (абзац, пункт, таблица, код)"},
	{"r (к)", "список ответов сессии, по одному на ваш запрос: стрелками выбрать, Enter или клик — открыть"},
	{"[ / ] (х / ъ)", "предыдущий / следующий ответ; к другому ответу — пока нет комментариев"},
	{"s (ы)", "собрать комментарии в промпт и вернуться в Claude Code"},
	{"ctrl+e", "открыть промпт (с собранными комментариями) в обычном редакторе"},
	{"q (й), ctrl+c", "выйти без изменений; с комментариями — двойным нажатием"},
	{"в поле комментария", "Enter сохраняет, пустой Enter отменяет, ⇧Enter (alt+Enter, ctrl+j) — новая строка, Esc — отмена; вставка и голосовой ввод — как в обычном поле"},
}

func (m model) helpRows() []row {
	accent, _, _, _ := m.palette()
	w := m.inner()
	left := min(42, w/2)
	var out []row
	for _, h := range helpKeys {
		keys, what := wrap(tr(h[0]), left-2), wrap(tr(h[1]), w-left)
		for k := 0; k < max(len(keys), len(what)); k++ {
			l, r := "", ""
			if k < len(keys) {
				l = keys[k]
			}
			if k < len(what) {
				r = what[k]
			}
			out = append(out, row{pad(accent.Render(l), left, lipgloss.NewStyle()) + r, -1, 0})
		}
	}
	return out
}

// view is how many body rows fit under the header.
func (m model) view() int { return max(1, m.height-len(m.header)) }

// fit moves the window only as far as keeping the cursor's rows in view needs, with a row
// of context around them; while a comment is typed, its box instead.
func (m *model) fit() {
	view := m.view()
	want := func(rw row) bool { return rw.line == m.r.Cursor && rw.edit == 0 }
	if m.picking {
		want = func(rw row) bool { return rw.line == m.sel }
	}
	first, last := -1, -1
	for i, rw := range m.rows {
		if want(rw) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if m.box >= 0 {
		first, last = m.box-1, m.box+m.field.Height() // the box's borders
		if cur := m.field.Cursor(); cur != nil && last-first+1 > view {
			first = m.box + cur.Y // a field taller than the window: its caret's row
			last = first
		}
	}
	if first >= 0 {
		if last-first+1 > view {
			last = first + view - 1
		}
		if first-1 < m.top {
			m.top = first - 1
		}
		if last+1 >= m.top+view {
			m.top = last + 2 - view
		}
	}
	m.top = max(0, min(m.top, len(m.rows)-view))
}

func (m model) View() tea.View {
	if m.outcome != "" || m.width == 0 {
		return tea.NewView("")
	}
	lines := append([]string{}, m.header...)
	for i := m.top; i < min(len(m.rows), m.top+m.view()); i++ {
		lines = append(lines, m.rows[i].text)
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	margin := strings.Repeat(" ", padX)
	for i := range lines {
		lines[i] = margin + lines[i]
	}
	v := tea.NewView(strings.Join(lines[:m.height], "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "inline-notes"
	if c := m.field.Cursor(); m.r.Draft != nil && c != nil && m.box >= m.top {
		c.X += padX + m.boxX
		c.Y += len(m.header) + m.box - m.top
		v.Cursor = c
	}
	return v
}

// editorArgv is the plain editor ctrl+e and a non-Claude file open: $FB_EDITOR, $EDITOR unless it is
// this program, else vi.
func editorArgv() []string {
	self, _ := os.Executable()
	for _, env := range []string{"FB_EDITOR", "EDITOR"} {
		if argv := strings.Fields(os.Getenv(env)); len(argv) > 0 {
			if p, err := exec.LookPath(argv[0]); err == nil && !sameFile(p, self) {
				return argv
			}
		}
	}
	return []string{"vi"}
}

func sameFile(a, b string) bool {
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}

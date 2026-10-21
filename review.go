package main

// The review as pure state transitions, ported from the inline-notes mod (hooks/review.ts):
// the same keys, the same blocks, the same prompt.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Comment is a note on lines Start..End, 0-based and inclusive.
type Comment struct {
	ID         int
	Start, End int
	Body       string
}

// Draft is the comment being typed: a new one (ID 0) or comment ID edited.
type Draft struct {
	ID         int
	Start, End int
}

// Block is the source lines the rendered view draws as one Markdown.
type Block struct{ Start, End int }

// Review is one reply under review. Methods return a changed copy; Text, Lines and Blocks never change.
type Review struct {
	Index    int // 1-based number of the reply among the session's text replies
	Text     string
	Lines    []string
	Blocks   []Block
	Cursor   int
	Anchor   int // the selection's other end; -1 without one
	Comments []Comment
	NextID   int
	Draft    *Draft
	Preview  bool   // the reply drawn rendered: the cursor walks blocks
	Armed    string // "cancel" after a first q with comments
	Status   string
	Help     bool
}

func linesOf(text string) []string {
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n")
}

// A heading, a list item or a fence opens a block of its own; a blank line closes one.
// Everything else joins the block above it, so a table's rows stay one table.
var (
	opens   = regexp.MustCompile(`^\s{0,3}(#{1,6}\s|[-*+]\s|\d+[.)]\s|` + "```" + `|~~~)`)
	heading = regexp.MustCompile(`^\s{0,3}#{1,6}\s`)
	fenceRe = regexp.MustCompile("^\\s{0,3}(`{3,}|~{3,})")
)

func blocksOf(lines []string) []Block {
	var blocks []Block
	fence := ""
	open, head := false, false
	for i, line := range lines {
		if fence != "" && len(blocks) > 0 {
			blocks[len(blocks)-1].End = i
			if strings.HasPrefix(strings.TrimSpace(line), fence) {
				fence = ""
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			open = false
			continue
		}
		if open && len(blocks) > 0 && !head && !opens.MatchString(line) {
			blocks[len(blocks)-1].End = i
		} else {
			blocks = append(blocks, Block{i, i})
		}
		open = true
		head = heading.MatchString(line)
		fence = ""
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			fence = m[1]
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, Block{0, 0})
	}
	return blocks
}

// blockIndex is the block holding a line, or the next one after a blank line.
func blockIndex(blocks []Block, line int) int {
	for i, b := range blocks {
		if line <= b.End {
			return i
		}
	}
	return len(blocks) - 1
}

func newReview(index int, text string) Review {
	lines := linesOf(text)
	return Review{Index: index, Text: text, Lines: lines, Blocks: blocksOf(lines), Anchor: -1, NextID: 1}
}

func lineRef(start, end int) string {
	if start == end {
		return fmt.Sprintf("L%d", start+1)
	}
	return fmt.Sprintf("L%d-L%d", start+1, end+1)
}

// currentRange is the lines a comment would take: the selection, or the cursor's line; whole blocks when rendered.
func (r Review) currentRange() (int, int) {
	from := r.Cursor
	if r.Anchor >= 0 {
		from = r.Anchor
	}
	start, end := min(from, r.Cursor), max(from, r.Cursor)
	if !r.Preview {
		return start, end
	}
	return r.Blocks[blockIndex(r.Blocks, start)].Start, r.Blocks[blockIndex(r.Blocks, end)].End
}

// commentsAtCursor is the comments touching the cursor's line, or its block when rendered.
func (r Review) commentsAtCursor() []Comment {
	r.Anchor = -1
	start, end := r.currentRange()
	var out []Comment
	for _, c := range r.Comments {
		if c.Start <= end && start <= c.End {
			out = append(out, c)
		}
	}
	return out
}

func (r Review) isSelected(line int) bool {
	start, end := r.currentRange()
	return r.Anchor >= 0 && start <= line && line <= end
}

// commentsOn counts the comments over lines start..end.
func (r Review) commentsOn(start, end int) int {
	n := 0
	for _, c := range r.Comments {
		if c.Start <= end && start <= c.End {
			n++
		}
	}
	return n
}

func (r Review) setCursor(line int) Review {
	r.Cursor = max(0, min(len(r.Lines)-1, line))
	if r.Preview {
		r.Cursor = r.Blocks[blockIndex(r.Blocks, r.Cursor)].Start // rendered, the cursor sits on a block's first line
	}
	r.Armed, r.Status = "", ""
	return r
}

func (r Review) moveBlock(by int) Review {
	i := max(0, min(len(r.Blocks)-1, blockIndex(r.Blocks, r.Cursor)+by))
	return r.setCursor(r.Blocks[i].Start)
}

func (r Review) toggleSelection() Review {
	r.Armed = ""
	if r.Anchor < 0 {
		r.Anchor = r.Cursor
		r.Status = fmt.Sprintf("Выделение с L%d: двигайте курсор, затем Enter. v снимает.", r.Cursor+1)
	} else {
		r.Anchor = -1
		r.Status = "Выделение снято."
	}
	return r
}

func (r Review) startAdd() Review {
	start, end := r.currentRange()
	// A blank line is easy to land on by accident; say so before the comment is typed.
	blank := true
	for _, l := range r.Lines[start : end+1] {
		blank = blank && strings.TrimSpace(l) == ""
	}
	r.Status = ""
	if blank {
		r.Status = lineRef(start, end) + " — пустая строка: в промпт она уйдёт как «∅»."
	}
	r.Draft, r.Armed = &Draft{Start: start, End: end}, ""
	return r
}

// startEdit opens comment id for editing (a click on it), or with id 0 the first at the cursor.
func (r Review) startEdit(id int) (Review, string) {
	var found *Comment
	if id == 0 {
		if at := r.commentsAtCursor(); len(at) > 0 {
			found = &at[0]
		}
	} else if i := slices.IndexFunc(r.Comments, func(c Comment) bool { return c.ID == id }); i >= 0 {
		found = &r.Comments[i]
	}
	if found == nil {
		r.Status = "На этой строке нет комментария."
		return r, ""
	}
	r.Draft, r.Armed, r.Status = &Draft{ID: found.ID, Start: found.Start, End: found.End}, "", ""
	return r, found.Body
}

func (r Review) saveDraft(value string) Review {
	d := r.Draft
	if d == nil {
		return r
	}
	r.Draft = nil
	body := strings.TrimSpace(value)
	if body == "" {
		r.Status = "Пустой комментарий отброшен."
		return r
	}
	r.Anchor = -1
	comments := slices.Clone(r.Comments)
	if d.ID != 0 {
		for i := range comments {
			if comments[i].ID == d.ID {
				comments[i].Body = body
			}
		}
		r.Comments, r.Status = comments, fmt.Sprintf("#%d изменён.", d.ID)
		return r
	}
	added := Comment{ID: r.NextID, Start: d.Start, End: d.End, Body: body}
	comments = append(comments, added)
	slices.SortStableFunc(comments, func(a, b Comment) int {
		if a.Start != b.Start {
			return a.Start - b.Start
		}
		if a.End != b.End {
			return a.End - b.End
		}
		return a.ID - b.ID
	})
	r.Comments, r.NextID = comments, r.NextID+1
	r.Status = fmt.Sprintf("#%d добавлен на %s.", added.ID, lineRef(added.Start, added.End))
	return r
}

func (r Review) cancelDraft() Review {
	r.Draft, r.Status = nil, "Комментарий не сохранён."
	return r
}

func (r Review) deleteAtCursor() Review {
	at := r.commentsAtCursor()
	if len(at) == 0 {
		r.Status = "На этой строке нет комментария."
		return r
	}
	id := at[0].ID
	r.Comments = slices.DeleteFunc(slices.Clone(r.Comments), func(c Comment) bool { return c.ID == id })
	r.Armed, r.Status = "", fmt.Sprintf("#%d удалён.", id)
	return r
}

func (r Review) jumpNext() Review {
	if len(r.Comments) == 0 {
		r.Status = "Комментариев пока нет."
		return r
	}
	// Past the cursor's block when rendered: a comment inside it would snap back to it.
	after := r.Cursor
	if r.Preview {
		r.Anchor = -1
		_, after = r.currentRange()
	}
	target := r.Comments[0]
	if i := slices.IndexFunc(r.Comments, func(c Comment) bool { return c.Start > after }); i >= 0 {
		target = r.Comments[i]
	}
	r = r.setCursor(target.Start)
	r.Status = fmt.Sprintf("#%d %s.", target.ID, lineRef(target.Start, target.End))
	return r
}

func (r Review) jumpPrev() Review {
	if len(r.Comments) == 0 {
		r.Status = "Комментариев пока нет."
		return r
	}
	target := r.Comments[len(r.Comments)-1]
	for i := len(r.Comments) - 1; i >= 0; i-- {
		if r.Comments[i].Start < r.Cursor {
			target = r.Comments[i]
			break
		}
	}
	r = r.setCursor(target.Start)
	r.Status = fmt.Sprintf("#%d %s.", target.ID, lineRef(target.Start, target.End))
	return r
}

// Outcomes a key can end the review with.
const (
	outSubmit = "submit"
	outCancel = "cancel"
)

// arm: s submits at once; q drops the review at once without comments, else on the second press.
func (r Review) arm(what string) (Review, string) {
	if what == outSubmit {
		if len(r.Comments) == 0 {
			r.Status = "Нечего отправлять: комментариев нет."
			return r, ""
		}
		return r, outSubmit
	}
	if len(r.Comments) == 0 || r.Armed == outCancel {
		return r, outCancel
	}
	r.Armed = outCancel
	r.Status = fmt.Sprintf("Нажмите q ещё раз, чтобы выбросить %s.", commentCount(len(r.Comments)))
	return r, ""
}

// commentCount: `1 комментарий`, `3 комментария`, `5 комментариев`.
func commentCount(n int) string {
	tens, ones := n%100, n%10
	form := "комментариев"
	switch {
	case tens >= 11 && tens <= 14:
	case ones == 1:
		form = "комментарий"
	case ones >= 2 && ones <= 4:
		form = "комментария"
	}
	return fmt.Sprintf("%d %s", n, form)
}

func quoted(lines []string, start, end int) string {
	const most = 12
	picked := lines[start : end+1]
	var out []string
	for i, line := range picked[:min(most, len(picked))] {
		if line == "" {
			line = "∅"
		}
		out = append(out, fmt.Sprintf("> %d: %s", start+i+1, line))
	}
	if len(picked) > most {
		out = append(out, fmt.Sprintf("> … ещё %d строк(и)", len(picked)-most))
	}
	return strings.Join(out, "\n")
}

func (r Review) buildPrompt() string {
	var blocks []string
	for i, c := range r.Comments {
		blocks = append(blocks, strings.Join([]string{
			fmt.Sprintf("%d. %s", i+1, lineRef(c.Start, c.End)), quoted(r.Lines, c.Start, c.End), "Комментарий:", c.Body,
		}, "\n"))
	}
	return fmt.Sprintf(`У меня есть inline-комментарии к твоему ответу #%d.

Пожалуйста, обработай их одним ответом: пройди по каждому пункту, объясни что меняешь/уточняешь, и если уместно — дай обновлённую версию ответа. Не игнорируй ни один комментарий.

%s`, r.Index, strings.Join(blocks, "\n\n"))
}

// The same physical keys on the Russian ЙЦУКЕН layout: о is j, л is k, П is G.
var ru = map[rune]rune{
	'о': 'j', 'л': 'k', 'м': 'v', 'с': 'c', 'у': 'e', 'в': 'd', 'т': 'n', 'ь': 'm', 'ы': 's', 'й': 'q', 'п': 'g', 'г': 'u',
	'х': '[', 'ъ': ']', 'к': 'r',
}

// keyName names a key layout-independently: `j`, `G`, `ctrl+d`, `enter`, `up`. `key` is
// what the terminal reports (bubbletea's Key.String()): `о`, `П`, `ctrl+в`, `shift+enter`.
func keyName(key string) string {
	mod, base := "", key
	if i := strings.LastIndex(key, "+"); i > 0 && i < len(key)-1 {
		mod, base = key[:i+1], key[i+1:]
	}
	runes := []rune(base)
	if len(runes) != 1 {
		return key
	}
	ch := runes[0]
	lower := unicode.ToLower(ch)
	latin := lower
	if l, ok := ru[lower]; ok {
		latin = l
	}
	if ch != lower || strings.Contains(mod, "shift+") {
		return strings.ReplaceAll(mod, "shift+", "") + string(unicode.ToUpper(latin))
	}
	return mod + string(latin)
}

// handleKey runs one key outside the comment field: the next state and, if it ends the review, the outcome.
func (r Review) handleKey(name string, page int) (Review, string) {
	if r.Help {
		r.Help = false // any key closes the help
		return r, ""
	}
	// Rendered, a step is a block and a page a few of them.
	by := func(lines int) Review {
		if !r.Preview {
			return r.setCursor(r.Cursor + lines)
		}
		step := 1
		if lines > 1 || lines < -1 {
			step = 5
		}
		if lines < 0 {
			step = -step
		}
		return r.moveBlock(step)
	}
	switch name {
	case "?":
		r.Help = true
	case "j", "down":
		return by(1), ""
	case "k", "up":
		return by(-1), ""
	case "ctrl+d", "pgdown", "space":
		return by(page), ""
	case "ctrl+u", "pgup":
		return by(-page), ""
	case "g", "home":
		return r.setCursor(0), ""
	case "G", "end":
		return r.setCursor(len(r.Lines)), ""
	case "v":
		return r.toggleSelection(), ""
	case "c", "enter":
		return r.startAdd(), ""
	case "d":
		return r.deleteAtCursor(), ""
	case "n":
		return r.jumpNext(), ""
	case "N":
		return r.jumpPrev(), ""
	case "m":
		r.Preview = !r.Preview
		return r.setCursor(r.Cursor), ""
	case "s":
		return r.arm(outSubmit)
	case "q":
		return r.arm(outCancel)
	}
	return r, ""
}

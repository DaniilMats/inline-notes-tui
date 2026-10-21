package main

import (
	"reflect"
	"strings"
	"testing"
)

const reply = "Флаки есть, но их не видно.\nПоход в базу один.\n\nPub/Sub влияет редко."

const md = "## Итог\nабзац один\nпродолжение\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\nx := 1\n\ny := 2\n```\n- пункт один\n  хвост\n- пункт два"

func step(t *testing.T, r Review, keys ...string) Review {
	t.Helper()
	for _, k := range keys {
		r, _ = r.handleKey(keyName(k), 2)
	}
	return r
}

func TestRangeCommentQuotesItsLines(t *testing.T) {
	r := newReview(3, reply).toggleSelection().setCursor(1).startAdd().saveDraft("  почему один?  ")
	if want := []Comment{{1, 0, 1, "почему один?"}}; !reflect.DeepEqual(r.Comments, want) {
		t.Fatalf("comments %v", r.Comments)
	}
	if r.Anchor != -1 {
		t.Fatal("the selection stays after the comment")
	}
	p := r.buildPrompt()
	if !strings.Contains(p, "ответу #3") || !strings.Contains(p, "1. L1-L2\n> 1: Флаки есть, но их не видно.\n> 2: Поход в базу один.\nКомментарий:\nпочему один?") {
		t.Fatalf("prompt:\n%s", p)
	}
}

func TestEditDeleteEmptyAndBlankLine(t *testing.T) {
	r := newReview(1, reply).setCursor(2).startAdd()
	if !strings.Contains(r.Status, "L3 — пустая строка") {
		t.Fatalf("status %q", r.Status)
	}
	r = r.saveDraft("x")
	if !strings.Contains(r.buildPrompt(), "> 3: ∅") {
		t.Fatal("a blank line is quoted as ∅")
	}
	r, body := r.startEdit(0)
	if body != "x" || r.Draft == nil || r.Draft.ID != 1 {
		t.Fatalf("edit opens the comment: %q %+v", body, r.Draft)
	}
	r = r.saveDraft("y")
	if r.Comments[0].Body != "y" {
		t.Fatal("edit keeps the comment's id and changes its body")
	}
	if r = r.startAdd().saveDraft("   "); len(r.Comments) != 1 || r.Status != "Пустой комментарий отброшен." {
		t.Fatal("an empty comment is dropped")
	}
	if len(r.deleteAtCursor().Comments) != 0 {
		t.Fatal("d deletes the comment at the cursor")
	}
}

func TestSubmitAndCancel(t *testing.T) {
	empty := newReview(1, reply)
	if _, out := empty.arm(outSubmit); out != "" {
		t.Fatal("nothing to submit without comments")
	}
	if _, out := empty.arm(outCancel); out != outCancel {
		t.Fatal("q leaves at once without comments")
	}
	r := empty.startAdd().saveDraft("x")
	if _, out := r.arm(outSubmit); out != outSubmit {
		t.Fatal("s submits")
	}
	once, out := r.arm(outCancel)
	if out != "" || !strings.Contains(once.Status, "выбросить 1 комментарий") {
		t.Fatalf("a first q with comments only arms: %q", once.Status)
	}
	if _, out := once.arm(outCancel); out != outCancel {
		t.Fatal("the second q cancels")
	}
	if moved := step(t, once, "j"); moved.Armed != "" {
		t.Fatal("any move disarms")
	}
}

func TestKeyNames(t *testing.T) {
	for in, want := range map[string]string{
		"о": "j", "л": "k", "с": "c", "ы": "s", "й": "q", "П": "G", "G": "G", "shift+g": "G",
		"ctrl+в": "ctrl+d", "ctrl+у": "ctrl+e", "enter": "enter", "shift+enter": "shift+enter", "х": "[", "ъ": "]",
		"?": "?", "ctrl+c": "ctrl+c",
	} {
		if got := keyName(in); got != want {
			t.Errorf("keyName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMovesInCyrillic(t *testing.T) {
	r := step(t, newReview(1, reply), "о", "П")
	if r.Cursor != 3 {
		t.Fatalf("j then G: %d", r.Cursor)
	}
	if r = step(t, r, "п"); r.Cursor != 0 {
		t.Fatal("g goes to the top")
	}
	if r = step(t, r, "ctrl+в"); r.Cursor != 2 {
		t.Fatalf("ctrl+d moves a page: %d", r.Cursor)
	}
	if r = step(t, r, "?"); !r.Help {
		t.Fatal("? opens the help")
	}
	if r = step(t, r, "j"); r.Help || r.Cursor != 2 {
		t.Fatal("any key closes the help and does nothing else")
	}
}

func TestBlocks(t *testing.T) {
	want := []Block{{0, 0}, {1, 2}, {4, 6}, {8, 12}, {13, 14}, {15, 15}}
	if got := blocksOf(linesOf(md)); !reflect.DeepEqual(got, want) {
		t.Fatalf("blocks %v", got)
	}
	if got := blocksOf(linesOf("")); !reflect.DeepEqual(got, []Block{{0, 0}}) {
		t.Fatalf("an empty reply has one block: %v", got)
	}
}

func TestRenderedCommentsTakeWholeBlocks(t *testing.T) {
	r := step(t, newReview(1, md), "m", "j", "j")
	if r.Cursor != 4 {
		t.Fatalf("j walks blocks: %d", r.Cursor)
	}
	r = r.startAdd().saveDraft("x")
	if !reflect.DeepEqual(r.Comments, []Comment{{1, 4, 6, "x"}}) {
		t.Fatalf("comments %v", r.Comments)
	}
	if !strings.Contains(r.buildPrompt(), "1. L5-L7\n> 5: | a | b |\n> 6: |---|---|\n> 7: | 1 | 2 |") {
		t.Fatal("the prompt quotes the whole block")
	}
	r = step(t, r, "v", "j")
	r = r.startAdd().saveDraft("y")
	if c := r.Comments[len(r.Comments)-1]; c.Start != 4 || c.End != 12 {
		t.Fatalf("a selection spans blocks: %+v", c)
	}
	r = step(t, r, "g", "n")
	if r.Cursor != 4 {
		t.Fatal("n goes to the first comment's block")
	}
	if step(t, r, "n").Cursor != 4 {
		t.Fatal("both start on the table: n wraps to it")
	}
	r = step(t, r, "m").setCursor(5)
	if r, _ = r.startEdit(0); r.Draft == nil || r.Draft.ID != 1 {
		t.Fatal("back in the source, a comment made on a block is found by its lines")
	}
}

func TestJumpsAndCounts(t *testing.T) {
	r := newReview(1, reply)
	for _, at := range []int{3, 1} {
		r = r.setCursor(at).startAdd().saveDraft("x")
	}
	if r = r.setCursor(0).jumpNext(); r.Cursor != 1 {
		t.Fatal("n goes to the next comment")
	}
	if r = r.jumpNext(); r.Cursor != 3 {
		t.Fatal("and the one after")
	}
	if r = r.jumpNext(); r.Cursor != 1 {
		t.Fatal("past the last, n wraps")
	}
	if r = r.jumpPrev(); r.Cursor != 3 {
		t.Fatal("N wraps back")
	}
	var got []string
	for _, n := range []int{1, 2, 4, 5, 11, 12, 21, 22, 25, 111} {
		got = append(got, commentCount(n))
	}
	want := []string{"1 комментарий", "2 комментария", "4 комментария", "5 комментариев", "11 комментариев",
		"12 комментариев", "21 комментарий", "22 комментария", "25 комментариев", "111 комментариев"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plurals %v", got)
	}
}

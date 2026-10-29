package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var cyrillic = regexp.MustCompile(`[А-Яа-яЁё]`)

// english switches the run to English for one test.
func english(t *testing.T) {
	t.Helper()
	if err := setLang("en"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lang = "ru" })
}

// Every Russian string in the code has its English in en, and en holds nothing else.
func TestEveryRussianStringHasEnglish(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	used := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "commentCount" {
				continue // the Russian plural forms; English has a branch of its own
			}
			if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.VAR && gen.Specs[0].(*ast.ValueSpec).Names[0].Name == "en" {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					s, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					if cyrillic.MatchString(s) {
						used[s] = true
						if _, ok := en[s]; !ok {
							t.Errorf("%s: no English for %q", fset.Position(lit.Pos()), s)
						}
					}
				}
				return true
			})
		}
	}
	for ru, english := range en {
		if !used[ru] {
			t.Errorf("en has %q, which the code no longer says", ru)
		}
		if cyrillic.MatchString(english) {
			t.Errorf("the English for %q is Russian: %q", ru, english)
		}
		if strings.Count(ru, "%") != strings.Count(english, "%") {
			t.Errorf("%q and %q format different arguments", ru, english)
		}
	}
}

func TestEnglish(t *testing.T) {
	if err := setLang("EN"); err == nil || lang != "ru" {
		t.Fatal("an unknown language keeps Russian and says so")
	}
	english(t)

	long := strings.Repeat("line\n", 14) + "last"
	r := newReview(3, long).toggleSelection().setCursor(14).startAdd().saveDraft("why?")
	if p := r.buildPrompt(); !strings.HasPrefix(p, "I have inline comments on your reply #3.\n\n") ||
		!strings.Contains(p, "> 12: line\n> … 3 more line(s)\nComment:\nwhy?") {
		t.Fatalf("the prompt:\n%s", p)
	}

	replies := []Reply{
		{Text: "The first reply.", Prompt: "first question"},
		{Text: "## Title\nparagraph one\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n- item", Prompt: "second question"},
	}
	next, _ := newModel(replies, 1, "").Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m := next.(model)
	var screens []string
	shot := func() { screens = append(screens, ansi.Strip(m.View().Content)) }
	shot() // rendered, the hints
	m = press(m, "enter")
	shot() // a comment's box and its placeholder
	m = press(typed(m, "x"), "enter")
	shot() // #1 added
	m = press(m, "m", "v", "j")
	shot() // the source, a selection
	m = press(m, "esc", "q")
	shot() // q armed
	m = press(m, "?")
	shot() // the help
	m = press(m, "j", "]")
	shot() // pinned to its reply
	m = press(m, "g", "d", "r")
	shot() // the list of replies
	for _, s := range screens {
		if loc := cyrillic.FindStringIndex(s); loc != nil {
			t.Fatalf("Russian on an English screen at %q:\n%s", s[loc[0]:min(len(s), loc[0]+40)], s)
		}
	}
	for i, want := range []string{
		"j/k block", "New comment on L1", "#1 added on L1.", "selection L1-L2", "Press q again to discard 1 comment.",
		"inline-notes · keys", "Comments belong to this reply", "session replies: 2",
	} {
		if !strings.Contains(screens[i], want) {
			t.Fatalf("screen %d has no %q:\n%s", i, want, screens[i])
		}
	}
}

package main

// The interface and the prompt speak Russian, the text written in the code, or English: every
// Russian string goes through tr, which swaps in its English twin from en. The inline-notes mod
// carries the same English for the same Russian.

import "fmt"

// langEnv names the language: ru (the default) or en.
const langEnv = "INLINE_NOTES_LANG"

// lang is the language of this run; main sets it once, before anything is drawn or printed.
// ponytail: a package variable, one language per process; a field on Review if one run ever needs two
var lang = "ru"

// setLang sets lang from the setting's value; an unknown one keeps Russian, and says so.
func setLang(value string) error {
	switch value {
	case "", "ru":
		lang = "ru"
	case "en":
		lang = "en"
	default:
		lang = "ru"
		return fmt.Errorf(tr("%s=%q: ожидается ru или en, остаётся русский"), langEnv, value)
	}
	return nil
}

// tr is text in lang. With args it is formatted; without, it stays a format a caller can
// hand fmt.Errorf, %w and all.
func tr(text string, args ...any) string {
	if lang == "en" {
		if english, ok := en[text]; ok {
			text = english
		}
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// en is the English for every Russian string in the code; a test keeps it complete.
var en = map[string]string{
	// main.go and transcript.go
	"номер ответа Claude (1 — первый); по умолчанию последний":                                                     "Claude's reply number (1 is the first); the last by default",
	"транскрипт сессии (.jsonl); по умолчанию сессия, из которой открыт Ctrl+G":                                    "the session transcript (.jsonl); by default the session Ctrl+G was opened from",
	"usage: inline-notes-tui [-n N] [-transcript FILE] [PROMPT_FILE]\nБез PROMPT_FILE промпт печатается в stdout.": "usage: inline-notes-tui [-n N] [-transcript FILE] [PROMPT_FILE]\nWithout PROMPT_FILE the prompt is printed to stdout.",
	"%s=%q: ожидается ru или en, остаётся русский":                                                                 "%s=%q: expected ru or en, Russian is kept",
	"в сессии ещё нет ответов Claude":                                                                              "the session has no replies from Claude yet",
	"inline-notes-tui: %v; открываю обычный редактор\n":                                                            "inline-notes-tui: %v; opening the plain editor\n",
	"inline-notes-tui: не удалось открыть редактор:":                                                               "inline-notes-tui: could not open the editor:",
	"нет транскриптов сессий для %s: %w":                                                                           "no session transcripts for %s: %w",
	"нет транскриптов сессий в %s":                                                                                 "no session transcripts in %s",
	"(продолжение после сжатия контекста)":                                                                         "(continued after the context was compacted)",

	// review.go: statuses
	"Выделение с L%d: двигайте курсор, затем Enter. v снимает.": "Selecting from L%d: move the cursor, then Enter. v clears it.",
	"Выделение снято.": "Selection cleared.",
	"%s — пустая строка: в промпт она уйдёт как «∅».": "%s is a blank line: the prompt will quote it as “∅”.",
	"На этой строке нет комментария.":                 "No comment on this line.",
	"Пустой комментарий отброшен.":                    "Empty comment dropped.",
	"#%d изменён.":                           "#%d updated.",
	"#%d добавлен на %s.":                    "#%d added on %s.",
	"Комментарий не сохранён.":               "Comment not saved.",
	"#%d удалён.":                            "#%d deleted.",
	"Комментариев пока нет.":                 "No comments yet.",
	"Нечего отправлять: комментариев нет.":   "Nothing to send: no comments.",
	"Нажмите q ещё раз, чтобы выбросить %s.": "Press q again to discard %s.",

	// review.go: the prompt
	"У меня есть inline-комментарии к твоему ответу #%d.\n\nПожалуйста, обработай их одним ответом: пройди по каждому пункту, объясни что меняешь/уточняешь, и если уместно — дай обновлённую версию ответа. Не игнорируй ни один комментарий.": "I have inline comments on your reply #%d.\n\nPlease address them in one reply: go through each point, explain what you change or clarify, and where it helps, give an updated version of the reply. Do not skip any comment.",
	"Комментарий:":      "Comment:",
	"… ещё %d строк(и)": "… %d more line(s)",

	// ui.go
	"Enter — сохранить · ⇧Enter — новая строка · Esc — отмена":        "Enter saves · ⇧Enter new line · Esc cancels",
	"Комментарии относятся к этому ответу: сначала s или удалите их.": "Comments belong to this reply: press s or delete them first.",
	"Других ответов в эту сторону нет.":                               "No more replies this way.",
	"Не удалось записать промпт: %v":                                  "Could not write the prompt: %v",
	"inline-notes · клавиши":                                          "inline-notes · keys",
	" (в скобках — русская раскладка; любая клавиша закрывает)":       " (any key closes; the keys work on the Russian layout too)",
	"inline-notes · ответы сессии: %d":                                "inline-notes · session replies: %d",
	" · по одному на ваш запрос":                                      " · one per prompt you sent",
	"строка %d/%d":                        "line %d/%d",
	"блок %d/%d (%s)":                     "block %d/%d (%s)",
	"выделение %s":                        "selection %s",
	"комментариев: %d":                    "comments: %d",
	"inline-notes · ответ #%d из %d «%s»": "inline-notes · reply #%d of %d “%s”",
	"строка":                              "line",
	"блок":                                "block",
	"выделить":                            "select",
	"коммент":                             "comment",
	"правка":                              "edit",
	"рендер":                              "render",
	"исходник":                            "source",
	"ответы":                              "replies",
	"в промпт":                            "to prompt",
	"отмена":                              "cancel",
	"все клавиши":                         "all keys",
	"выбрать":                             "choose",
	"открыть":                             "open",
	"назад":                               "back",
	"сохранить":                           "save",
	"новая строка":                        "new line",
	"Новый комментарий к %s":              "New comment on %s",
	"Правка #%d к %s":                     "Editing #%d on %s",
	"(без запроса)":                       "(no prompt)",
	" · открыт":                           " · open",

	// ui.go: the help
	"j / k, ↑ / ↓ (о / л)":                  "j / k, ↑ / ↓",
	"строка вниз / вверх; в рендере — блок": "line down / up; a block when rendered",
	"g / G, Home / End (п / П)":             "g / G, Home / End",
	"в начало / в конец":                    "top / bottom",
	"ctrl+d / ctrl+u, PgDn / PgUp, пробел":  "ctrl+d / ctrl+u, PgDn / PgUp, space",
	"страница вниз / вверх":                 "page down / up",
	"v (м)":                                 "v",
	"начать или снять выделение диапазона; Esc снимает": "start or clear a range selection; Esc clears it",
	"Enter, c (с), клик по строке с курсором":           "Enter, c, a click on the cursor's line",
	"комментарий к строке или диапазону":                "comment on the line or range",
	"e (у), клик по комментарию":                        "e, a click on a comment",
	"править комментарий":                               "edit the comment",
	"d (в)":                                             "d",
	"удалить комментарий на строке":                     "delete the comment on the line",
	"n / N (т / Т)": "n / N",
	"следующий / предыдущий комментарий": "next / previous comment",
	"m (ь)": "m",
	"исходник или отрисованный markdown (открывается он): в нём курсор и комментарии — по блокам (абзац, пункт, таблица, код)": "source or rendered Markdown (it opens rendered): there the cursor and comments take blocks (a paragraph, list item, table, code)",
	"r (к)": "r",
	"список ответов сессии, по одному на ваш запрос: стрелками выбрать, Enter или клик — открыть": "the session's replies, one per prompt you sent: the arrows choose, Enter or a click opens",
	"[ / ] (х / ъ)": "[ / ]",
	"предыдущий / следующий ответ; к другому ответу — пока нет комментариев": "previous / next reply; another reply opens only while there are no comments",
	"s (ы)": "s",
	"собрать комментарии в промпт и вернуться в Claude Code":          "put the comments in the prompt and return to Claude Code",
	"открыть промпт (с собранными комментариями) в обычном редакторе": "open the prompt, comments included, in the plain editor",
	"q (й), ctrl+c": "q, ctrl+c",
	"выйти без изменений; с комментариями — двойным нажатием": "leave without changes; with comments, press twice",
	"в поле комментария": "in a comment's field",
	"Enter сохраняет, пустой Enter отменяет, ⇧Enter (alt+Enter, ctrl+j) — новая строка, Esc — отмена; вставка и голосовой ввод — как в обычном поле": "Enter saves, an empty Enter cancels, ⇧Enter (alt+Enter, ctrl+j) starts a new line, Esc cancels; paste and dictation work as in any field",
}

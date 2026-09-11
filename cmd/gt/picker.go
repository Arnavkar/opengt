package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

const pickerPrompt = "Checkout a branch (autocomplete or arrow keys)"
const deleteBranchPrompt = "Delete a branch (autocomplete or arrow keys)"
const deletePrompt = "Delete branches (space to toggle, enter to confirm)"

func filterRows(rows []pickRow, q string) []pickRow {
	if q == "" {
		return rows
	}
	ql := strings.ToLower(q)
	var out []pickRow
	for _, r := range rows {
		if strings.Contains(strings.ToLower(rowSearchText(r)), ql) {
			out = append(out, r)
		}
	}
	return out
}

func rowSearchText(r pickRow) string {
	if r.openGithub {
		return r.text
	}
	return r.branch + " " + r.detail + " " + r.text
}

func pickBranch(rows []pickRow) (pickRow, error) {
	return pickOne(rows, pickerPrompt, "checkout cancelled")
}

func pickOne(rows []pickRow, prompt, cancel string) (pickRow, error) {
	chosen, err := runPicker(rows, pickerMode{prompt: prompt, cancel: cancel})
	if err != nil {
		return pickRow{}, err
	}
	if len(chosen) == 0 {
		return pickRow{}, fmt.Errorf("%s", cancel)
	}
	return chosen[0], nil
}

func pickMulti(rows []pickRow, prompt string) ([]pickRow, error) {
	return runPicker(rows, pickerMode{prompt: prompt, multi: true, cancel: "cancelled"})
}

type pickerMode struct {
	prompt string
	multi  bool
	cancel string
}

func runPicker(rows []pickRow, mode pickerMode) ([]pickRow, error) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	defer term.Restore(fd, old)

	query := ""
	visible := rows
	sel := 0
	for i, r := range rows {
		if r.current {
			sel = i
			break
		}
	}
	checked := map[string]bool{}
	if mode.multi {
		for _, r := range rows {
			checked[r.branch] = true
		}
	}

	lastHeight := 0
	clear := func() {
		if lastHeight == 0 {
			return
		}
		fmt.Fprintf(os.Stderr, "\x1b[%dA\x1b[0J", lastHeight)
	}
	defer func() {
		clear()
		fmt.Fprint(os.Stderr, "\x1b[?25h")
	}()

	draw := func() {
		clear()
		var b strings.Builder
		b.WriteString("\r\x1b[?25l\x1b[2K")
		if useColor() {
			b.WriteString("\x1b[36m?\x1b[0m ")
			b.WriteString(mode.prompt)
			b.WriteString(" \x1b[2m›\x1b[0m ")
		} else {
			b.WriteString("? ")
			b.WriteString(mode.prompt)
			b.WriteString(" › ")
		}
		b.WriteString(query)
		b.WriteString("\r\n")
		for i, r := range visible {
			b.WriteString("\x1b[2K")
			if i == sel {
				if useColor() {
					b.WriteString("\x1b[32m❯\x1b[0m ")
				} else {
					b.WriteString("> ")
				}
			} else {
				b.WriteString("  ")
			}
			if mode.multi {
				if checked[r.branch] {
					b.WriteString("[x] ")
				} else {
					b.WriteString("[ ] ")
				}
			}
			b.WriteString(r.render(useColor(), i == sel))
			b.WriteString("\r\n")
		}
		if mode.multi {
			b.WriteString("\x1b[2K")
			hint := "space toggle · enter delete selected · esc keep all"
			if useColor() {
				b.WriteString(paint("\x1b[2m", hint))
			} else {
				b.WriteString(hint)
			}
			b.WriteString("\r\n")
		}
		fmt.Fprint(os.Stderr, b.String())
		lastHeight = 1 + len(visible)
		if mode.multi {
			lastHeight++
		}
	}

	draw()

	buf := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return nil, fmt.Errorf("%s", mode.cancel)
		}
		refit := false
		move := 0
		switch {
		case buf[0] == 3, buf[0] == 27 && n == 1:
			return nil, fmt.Errorf("%s", mode.cancel)
		case buf[0] == '\r', buf[0] == '\n':
			if mode.multi {
				var out []pickRow
				for _, r := range rows {
					if checked[r.branch] {
						out = append(out, r)
					}
				}
				return out, nil
			}
			if len(visible) == 0 {
				continue
			}
			return []pickRow{visible[sel]}, nil
		case buf[0] == 127, buf[0] == 8:
			query = trimLastRune(query)
			refit = true
		case buf[0] == 14:
			move = 1
		case buf[0] == 16:
			move = -1
		case n >= 3 && buf[0] == 27 && buf[1] == '[':
			switch buf[2] {
			case 'A':
				move = -1
			case 'B':
				move = 1
			}
		case mode.multi && n == 1 && buf[0] == ' ':
			if len(visible) > 0 {
				name := visible[sel].branch
				checked[name] = !checked[name]
			}
		case n == 1 && buf[0] >= 32 && buf[0] < 127:
			query += string(buf[0])
			refit = true
		default:
			continue
		}
		if refit {
			visible = filterRows(rows, query)
			if sel >= len(visible) {
				sel = max(0, len(visible)-1)
			}
		}
		if move != 0 && len(visible) > 0 {
			sel += move
			if sel < 0 {
				sel = 0
			}
			if sel >= len(visible) {
				sel = len(visible) - 1
			}
		}
		draw()
	}
}

func trimLastRune(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return string(r[:len(r)-1])
}

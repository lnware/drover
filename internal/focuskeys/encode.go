// Package focuskeys turns Bubble Tea key presses into what tmux send-keys
// needs, so focus mode can drive an agent pane from the TUI.
package focuskeys

import (
	"regexp"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

type Kind int

const (
	KindNone  Kind = iota // key has no tmux equivalent; drop it
	KindText              // literal text (send-keys -l)
	KindKeys              // tmux key names
	KindPaste             // one bracketed paste
)

type Input struct {
	Kind Kind
	Text string
	Keys []string
}

var names = map[string]string{
	"enter":     "Enter",
	"tab":       "Tab",
	"backspace": "BSpace",
	"esc":       "Escape",
	"up":        "Up",
	"down":      "Down",
	"left":      "Left",
	"right":     "Right",
	"home":      "Home",
	"end":       "End",
	"pgup":      "PageUp",
	"pgdown":    "PageDown",
	"delete":    "DC",
	"insert":    "IC",
	"space":     "Space",
}

var fKey = regexp.MustCompile(`^f([0-9]{1,2})$`)

// Encode maps one key press. Printable text stays literal so that Finnish
// letters and symbols never depend on tmux key-name tables.
//
// Bubble Tea fills Key.Text only for a plain printable key: with Ctrl or Alt
// held the key is Code plus Mod and Text is empty, so Alt+letter is rebuilt
// from the code (and the shifted code, for Alt+Shift+letter).
func Encode(msg tea.KeyPressMsg) Input {
	k := tea.Key(msg)
	if k.Text != "" && k.Mod&^tea.ModShift == 0 {
		return Input{Kind: KindText, Text: k.Text}
	}
	if k.Mod&^tea.ModShift == tea.ModAlt && unicode.IsPrint(k.Code) {
		if k.Code == ' ' {
			return Input{Kind: KindKeys, Keys: []string{"M-Space"}}
		}
		char := k.Code
		if k.Mod&tea.ModShift != 0 {
			// The kitty keyboard protocol only reports the shifted code when
			// asked to; without it a letter is still known to be a capital.
			switch {
			case k.ShiftedCode != 0:
				char = k.ShiftedCode
			case unicode.IsLetter(k.Code):
				char = unicode.ToUpper(k.Code)
			}
		}
		return Input{Kind: KindKeys, Keys: []string{"M-" + string(char)}}
	}
	return encodeNamed(k.Keystroke())
}

// EncodePaste maps a bracketed paste, which Bubble Tea delivers as its own
// message rather than as a key.
func EncodePaste(content string) Input {
	return Input{Kind: KindPaste, Text: content}
}

// encodeNamed handles Bubble Tea's keystroke form: "ctrl+a", "alt+up",
// "shift+tab", "ctrl+shift+left", "f5".
func encodeNamed(s string) Input {
	var ctrl, alt, shift bool
	for {
		switch {
		case strings.HasPrefix(s, "ctrl+"):
			ctrl, s = true, s[len("ctrl+"):]
		case strings.HasPrefix(s, "alt+"):
			alt, s = true, s[len("alt+"):]
		case strings.HasPrefix(s, "shift+"):
			shift, s = true, s[len("shift+"):]
		default:
			return finish(s, ctrl, alt, shift)
		}
	}
}

func finish(base string, ctrl, alt, shift bool) Input {
	name, ok := names[base]
	switch {
	case ok:
	case fKey.MatchString(base):
		name = "F" + fKey.FindStringSubmatch(base)[1]
	case ctrl && len([]rune(base)) == 1:
		name = strings.ToLower(base) // C-a, C-], C-@
	default:
		return Input{}
	}
	if shift && name == "Tab" {
		name, shift = "BTab", false
	}
	prefix := ""
	if ctrl {
		prefix += "C-"
	}
	if alt {
		prefix += "M-"
	}
	if shift {
		prefix += "S-"
	}
	return Input{Kind: KindKeys, Keys: []string{prefix + name}}
}

package tmux

import (
	"strconv"
	"strings"
)

// target addresses a session's active pane by exact session name.
func target(name string) string { return "=" + name + ":" }

// Capture is one snapshot of a pane: tmux has already done the terminal
// emulation, so Lines carry the rendered screen with SGR escapes.
type Capture struct {
	Lines         []string
	CursorX       int
	CursorY       int
	CursorVisible bool
	// Alt is set while the program runs on the alternate screen (Claude Code
	// in fullscreen mode, Helix, lazygit, vim): tmux keeps no history for it,
	// so scrollback must be done by the program itself. Mouse says whether it
	// asked for mouse reports, and MouseSGR whether in the SGR encoding.
	Alt, Mouse, MouseSGR bool
}

// Capture snapshots the session's pane and cursor in one tmux invocation.
func (d Driver) Capture(name string) (Capture, error) {
	t := target(name)
	out, err := d.run(
		"capture-pane", "-p", "-e", "-t", t, ";",
		"display-message", "-p", "-t", t,
		"#{cursor_x} #{cursor_y} #{cursor_flag} #{alternate_on} #{mouse_any_flag} #{mouse_sgr_flag}",
	)
	if err != nil {
		return Capture{}, err
	}
	lines := strings.Split(out, "\n")
	c := Capture{Lines: lines[:len(lines)-1]}
	f := strings.Fields(lines[len(lines)-1])
	if len(f) >= 6 {
		c.CursorX, _ = strconv.Atoi(f[0])
		c.CursorY, _ = strconv.Atoi(f[1])
		c.CursorVisible = f[2] == "1"
		c.Alt, c.Mouse, c.MouseSGR = f[3] == "1", f[4] == "1", f[5] == "1"
	}
	return c, nil
}

// Resize makes the session's pane cols x rows. With no client attached the
// window is exactly this size and the status line takes no row from it.
func (d Driver) Resize(name string, cols, rows int) error {
	_, err := d.run("resize-window", "-t", target(name),
		"-x", strconv.Itoa(max(cols, 1)), "-y", strconv.Itoa(max(rows, 1)))
	return err
}

// PrepareAttach hands window sizing back to the attaching client; Resize
// pins the size manually, which a real terminal must not inherit.
func (d Driver) PrepareAttach(name string) error {
	_, err := d.run("set-option", "-w", "-t", target(name), "window-size", "latest")
	return err
}

// SendKeys sends tmux key names (Enter, C-a, M-Up, ...) to the pane.
func (d Driver) SendKeys(name string, keys ...string) error {
	args := []string{"send-keys", "-t", target(name), "--"}
	for _, k := range keys {
		args = append(args, literalArg(k)) // "M-;" is a key name, not a separator
	}
	_, err := d.run(args...)
	return err
}

// literalArg protects an argument that is data from tmux's command parser,
// which reads a trailing ';' as the end of a command: send-keys -l -- ";"
// sends nothing, and "x;" sends "x". Preceding the semicolon with a backslash
// makes tmux keep it (it removes exactly one backslash, so text that already
// ends in "\;" survives too). Arguments that merely contain a semicolon
// need nothing.
func literalArg(s string) string {
	if strings.HasSuffix(s, ";") {
		return s[:len(s)-1] + `\;`
	}
	return s
}

// SendText types text literally, so it is never read as key names.
func (d Driver) SendText(name, text string) error {
	_, err := d.run("send-keys", "-l", "-t", target(name), "--", literalArg(text))
	return err
}

// Paste delivers text as one paste, bracketed when the program asked for it.
func (d Driver) Paste(name, text string) error {
	const buf = "drover-paste"
	_, err := d.run(
		"set-buffer", "-b", buf, "--", literalArg(text), ";",
		"paste-buffer", "-p", "-d", "-b", buf, "-t", target(name),
	)
	return err
}

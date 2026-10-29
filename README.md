# drover

A terminal UI for coding-agent sessions, one per git worktree. Sessions live on
a private tmux server, so they survive the UI and never touch your own tmux.
Built for [Claude Code](https://claude.com/claude-code); other agents run
unchanged.

Made for the bare-repo layout:

```
project/
  .bare/
  main/
  feature/
    feature-a/
  bug/
    bug1/
```

## Requirements

Go, `tmux` and `git`. Optional: `lazygit` (the `G` popup), a Nerd Font (the
`icons = "nerd"` branch glyph).

## Install and run

```sh
go build -o bin/drover . && ./bin/drover ~/dev/acme/infra
```

Project roots can be given as arguments or listed in
`~/.config/drover/config.toml`:

```toml
projects = [
"~/path/to/project/infra",
"/path/to/project/app1"
]
agent_cmd = "claude"   # started in each worktree
socket = "drover"      # private tmux server name (tmux -L)
notify = false         # desktop notification when an unfocused agent needs you
resume_args = ""       # appended by `v`; defaults to --continue when agent_cmd is claude
icons = "unicode"      # or "nerd" for the Nerd Font branch glyph in the git bar
theme = "jetbrains"    # or "catppuccin" (mocha), "catppuccin-macchiato", "catppuccin-frappe"
mouse = true           # click and wheel support; false leaves the mouse to the terminal
zoom_key = "ctrl+z"    # focus mode: zoom the pane to the whole terminal, and back
terminal_cmd = ""      # `T`: defaults to $SHELL
editor_cmd = ""        # `o`: defaults to `hx .`, else `$EDITOR .`, else `vi .`
lazygit_cmd = ""       # `G`: defaults to `lazygit`
```

`c` opens the config in `$VISUAL` / `$EDITOR` and reloads it when the editor
exits; `R` reloads without opening it. A reload applies everything except
`socket` and `mouse`, which are fixed at start-up (the footer says so).

## Keys

Press `?` in drover for the full list. The main ones:

| Key                 | Action                                                                 |
| ------------------- | ---------------------------------------------------------------------- |
| `↑/k` `↓/j`         | move                                                                   |
| `enter`             | focus the agent, starting it if needed; on a header, fold or unfold it |
| `←/h` `→/l`         | collapse / expand the section you are in                               |
| `F`                 | fold every section, or unfold them all                                 |
| `ctrl+\` / `ctrl+q` | leave focus mode (every other key goes to the pane)                    |
| `ctrl+z`            | in focus mode: zoom the pane to the whole terminal, and back           |
| `PgUp` / `PgDn`     | scroll the output back / forward (MacBook: `Fn+↑` / `Fn+↓`)            |
| `End` / `q` / `esc` | back to live output while scrolled                                     |
| `n`                 | new worktree: type `feature/my-change`, enter creates it               |
| `v`                 | resume the last Claude conversation in this worktree                   |
| `d`                 | remove the worktree (press twice; the branch is kept)                  |
| `/`                 | search; filters as you type, `enter` keeps it, `esc` clears            |
| `w`                 | show only agents that are waiting, done or in error                    |
| `r`                 | rename: a display label (git, folders and tmux are untouched)          |
| `M`                 | rename the branch and move the folder (stopped sessions only)          |
| `T` / `o`           | start a terminal / editor for this worktree                            |
| `G`                 | lazygit in a popup over the screen                                     |
| `a`                 | jump to the agent (the worktree row)                                   |
| `A`                 | attach what the pane shows full screen; `C-a d` returns                |
| `x` / `X`           | kill what the cursor is on / everything (`X` twice)                    |
| `c` / `R`           | edit / reload the config                                               |
| `q`                 | quit; sessions keep running                                            |

The footer shows the keys that apply to the selected row and drops the rarer
ones when the terminal is narrow.

## How it works

**Sessions.** Each worktree gets one tmux session on a private server (socket
`drover`, prefix `C-a`). Leave drover and the agents keep running. `drover
stop` ends the server and every session on it.

**Status.** Working / waiting / done comes from Claude Code hooks, with nothing
to install: drover starts Claude as `claude --settings
~/.local/state/drover/claude-settings.json`, which Claude merges with your own
settings. Your `~/.claude/settings.json` is never read or changed. `drover
hooks` prints what is injected. If `agent_cmd` is a wrapper, put `{settings}`
where the path should go. Other agents show as plain `running`.

**Layout.** Worktrees are grouped by the folder they sit in under the project
root (`feature/...`, `bug/...`; the project's own folders are `base`). Projects
are headed by the folder that holds them, as a filled bar, when your projects
live in more than one parent folder. Sections fold at three levels (org,
project, group), folds are remembered, and a folded header shows a summary of
the states it hides.

**Worktrees.** `n` creates `<project>/<branch>` from `origin/HEAD` (else
`origin/main`, `master`) as of your last fetch; drover never touches the
network. `d` is a plain `git worktree remove`, which refuses a dirty tree, and
it is blocked while a session runs there. Branches are never deleted. `M`
renames the branch and moves the folder, checking everything first and undoing
the branch rename if the move fails; the remote branch is not renamed and
nothing is pushed.

**Terminal, editor and lazygit.** `T` and `o` start a terminal or editor in the
worktree's directory, shown as child rows under it; each is its own tmux
session and persists like an agent. `G` opens lazygit as a popup that exists
only while open. `d` and `M` refuse while anything runs in the worktree.

A pane drover shows has no outer terminal behind it. Programs that ask for one
through tmux passthrough (yazi does, to detect terminal features) would wait for
an answer that never comes, so drover answers the device-attributes query for
the companion you are focused on. Without that, yazi takes five seconds to quit.

**Search.** `/` matches every word you type against the label, branch, project,
session name and state. Labels set with `r` are stored in
`~/.local/state/drover/aliases.json`.

**Git status.** The selected worktree gets a gitmux-style bar, and rows carry
compact markers: `↑`/`↓` ahead/behind, `●` staged, `✚` modified, `…` untracked,
`✖` conflicts, `✔` clean, `◌` no upstream. Git is read sparingly (a few
processes a minute while idle), and never takes the index lock an agent needs.

**Scrollback.** The right pane is a `tmux capture-pane` snapshot. Scroll it
with the wheel or `PgUp`. Programs that draw on the alternate screen (Claude
Code in fullscreen mode, Helix, lazygit, vim) keep no tmux history, so drover
passes the wheel and page keys to them instead.

**Mouse.** Clicks select rows, fold headers and focus the pane; the wheel
moves the list or scrolls the pane. Capturing the mouse stops the terminal's
own text selection (kitty: hold `shift` to select); `mouse = false` turns it
off.

**When nobody is looking.** Inside tmux, drover pauses all polling while its
popup or window is hidden, and resumes at once when it is shown again. With
`notify = true` it still raises a notification when an agent needs you.

**Zoom.** In focus mode `ctrl+z` gives the pane the whole terminal; the same
key restores the split. `zoom_key` changes it (Bubble Tea key names such as
`"f10"`; `ctrl+\` and `ctrl+q` are reserved for leaving focus). `A` is the other
full screen: a real tmux attach with no capture in between.

## Themes

`theme` picks the palette: `jetbrains` (the default, from a Helix theme),
`catppuccin-mocha` (also `catppuccin`), `catppuccin-macchiato` or
`catppuccin-frappe`. drover colours text but does not paint the background, so
pair it with your terminal's matching theme. Every palette is tested for
contrast on the background and on the selected row, with 256-colour fallbacks
for terminals without truecolor.

## Licence

[MIT](LICENCE).

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
background = true     # paint the list, title bar and footer in the theme colour
mouse = true           # click and wheel support; false leaves the mouse to the terminal
zoom_key = "ctrl+z"    # focus mode: zoom the pane to the whole terminal, and back
next_key = ""          # optional key for the next agent that needs you (shift+ for the previous)
context_key = ""       # optional key for the next context (shift+ for the previous), e.g. "f11"
switch_key = "f12"     # opens the view switcher (agents, terminal, editor); F1-F9 jump
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
| `enter`             | focus the agent; on a worktree with none, name and start one; on a stopped agent, resume it; on a header, fold it |
| `←/h` `→/l`         | collapse / expand the section you are in                               |
| `F`                 | fold every section, or unfold them all                                 |
| `ctrl+\` / `ctrl+q` | leave focus mode (every other key goes to the pane)                    |
| `ctrl+z`            | in focus mode: zoom the pane to the whole terminal, and back           |
| `F12`               | open the switcher popup; `←`/`→` change its scope (views, worktrees, projects, contexts), `F12`/`↑↓` choose, `enter` goes, `esc` cancels (list and focus mode) |
| `tab` / `shift+tab` | go to the next / previous agent that waits, finished or failed, in this context (list; `next_key` sets a key that also works in focus mode) |
| `:`                 | the command line (list only): `ctx`, `wt`, `next`, `term`, `edit`, `new`, `config`, `reload`, `help`, `q` |
| `F1`-`F9` / `ctrl+1`-`9` | go straight to view 1-9, starting the terminal or editor if needed |
| `PgUp` / `PgDn`     | scroll the output back / forward (MacBook: `Fn+↑` / `Fn+↓`)            |
| `End` / `q` / `esc` | back to live output while scrolled                                     |
| `n`                 | new worktree: type `feature/my-change`, enter creates it               |
| `N`                 | new agent session in this worktree: asks for a name (empty: named after the branch) |
| `v`                 | resume the stopped agent under the cursor with its own conversation    |
| `d`                 | remove the worktree (press twice; the branch is kept)                  |
| `/`                 | search; filters as you type, `enter` keeps it, `esc` clears            |
| `w`                 | show only agents that are waiting, done or in error                    |
| `r`                 | rename an agent, or a worktree's display label (git and folders are untouched) |
| `M`                 | rename the branch and move the folder (stopped sessions only)          |
| `T` / `o`           | start a terminal / editor for this worktree                            |
| `G`                 | lazygit in a popup over the screen                                     |
| `a`                 | back to the agent from a terminal or editor row                        |
| `A`                 | attach what the pane shows full screen; `C-a d` returns                |
| `x` / `X`           | stop what the cursor is on (a stopped agent: forget it) / everything (`X` twice) |
| `c` / `R`           | edit / reload the config                                               |
| `q`                 | quit; sessions keep running                                            |

The footer shows the keys that apply to the selected row and drops the rarer
ones when the terminal is narrow.

## How it works

**Sessions.** A worktree can run several agents, each a tmux session on a
private server (socket `drover`, prefix `C-a`) named `<project>/<branch>~<id>`.
Leave drover and the agents keep running. `drover stop` ends the server and
every session on it.

Each agent is a row under its worktree, with an icon for the kind of session
(`◈` agent, `$` terminal, `✎` editor; Nerd Font glyphs with `icons = "nerd"`),
its name, and its state and age. `N` asks for a name; leave it empty and the
session is named after the branch (`feature-a`, then `feature-a-2`, ...). A
session is a small record in `~/.local/state/drover/sessions.json`: its id, its
name and, for Claude, the conversation id. drover starts Claude with
`--session-id <uuid> --name <name>`, so `x` stops an agent without losing its
row, and `enter` or `v` on it brings it back with `--resume <uuid>`: that very
conversation, not whichever ran last in the folder. `x` on a stopped agent
forgets it. Removing a worktree forgets its sessions. The flags are added when
`agent_cmd` starts with `claude`; other agents get the status hooks only. A
session started by an earlier drover (named without an id) is adopted as an
agent named after the branch.

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

**Switching views.** One key, in the list and in focus mode: `F12` opens a switcher
over the worktree's views (its agents, then terminal and editor), starting on the
next one; `F12` again moves on, `enter` or a number goes there, `esc` cancels.
`F1`-`F9` jump to the nth view at once, and the pane header numbers them.
The popup opens on the scope the cursor is on: **Views** on a worktree,
**Worktrees** on a group header, **Projects** on a project or org header, with
that header's own entry chosen. In it `←`/`→` change the scope: **Views** (this worktree's agents,
terminal and editor, picked by number), **Worktrees** (every worktree of every
project, those needing attention first; type to filter) and **Projects** (type to
filter; `enter` goes to the project's first worktree). Going to a worktree
clears a filter that hides it and opens the sections that fold it. **Contexts**
lists the contexts with their worktree and waiting counts; `enter` switches. Function keys are the
default because they reach drover through kitty and tmux unchanged
(MacBook: `Fn+F12`, or set the keyboard to use F-keys as standard). `ctrl+tab`,
`ctrl+shift+tab` and `ctrl+1`-`9` work as well when the terminal delivers them,
which needs `extended-keys` in tmux and, in kitty, an unmapped `ctrl+tab`.
`switch_key` changes the key that opens the switcher.

**Contexts.** Drover can show one thing at a time. With more than one org the
list shows a single org, the others are tabs in the title bar (a `◆2` after a
tab says two agents there wait on you), and `:ctx` switches between them (see
below). The cursor returns to where you were in each, and the active context is
remembered between runs. Notifications still arrive as before; in addition, what
happened in a context while you were elsewhere is listed when you enter it.
The worktree, project and attention lists in the switcher stay inside the active
context. To group differently, name the contexts in the config with project paths. A
path takes a project when it is the project's root or any folder above it, so
`~/dev/acme` takes everything under it (which is what an org is: the folder that
holds the project). Whatever no context takes ends up in an "Other" one, and a
path that takes no project is reported when drover starts and on reload:

```toml
[[context]]
name = "Work"
projects = ["~/dev/acme"]

[[context]]
name = "Free time"
projects = ["~/dev/personal/garden-app", "~/dev/personal/home-lab"]
```

**The command line.** `:` in the list opens a prompt in the footer with the
matching commands above it: `tab` completes, `↑`/`↓` choose, `enter` runs the
chosen one, `esc` cancels, and `↑` on an empty line recalls the last command.

| command         | does                                                             |
| --------------- | ---------------------------------------------------------------- |
| `ctx [name\|-]` | switch context: the next one, a name (a prefix is enough), or back |
| `wt <name>`     | jump to a worktree of this context by project or branch          |
| `next`          | go to the next agent that needs you                              |
| `agent` `term` `edit` | show the agent, open the terminal or the editor            |
| `new` `config` `reload` `help` `q` | the keys `n`, `c`, `R`, `?` and `q`            |

In focus mode `:` goes to the pane, so Helix keeps it. `context_key` sets an
optional key for the next context (`shift+` it for the previous one) if you want
one; there is none by default.

## Themes

`theme` picks the palette: `jetbrains` (the default, from a Helix theme),
`catppuccin-mocha` (also `catppuccin`), `catppuccin-macchiato` or
`catppuccin-frappe`. By default drover paints the title bar, the list and the
footer in the theme's background, so a terminal background image or opacity
does not show through them; the agent pane keeps your terminal's background.
`background = false` leaves everything unpainted. Pair the theme with your
terminal's matching one. Every palette is tested for
contrast on the background and on the selected row, with 256-colour fallbacks
for terminals without truecolor.

## Licence

[MIT](LICENCE).

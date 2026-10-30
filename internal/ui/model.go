// Package ui is the Bubble Tea split view: worktrees and sessions on the left,
// the selected agent's live pane on the right, and a focus mode that forwards
// keys to it.
package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lnware/drover/internal/alias"
	"github.com/lnware/drover/internal/config"
	"github.com/lnware/drover/internal/gitinfo"
	"github.com/lnware/drover/internal/notify"
	"github.com/lnware/drover/internal/session"
	"github.com/lnware/drover/internal/status"
	"github.com/lnware/drover/internal/tmux"
	"github.com/lnware/drover/internal/worktree"
)

const (
	refreshEvery = 2 * time.Second
	// statusPoll reads the hook status files; they are a handful of tiny
	// files, so a short interval is cheap and keeps the list feeling live.
	statusPoll = 300 * time.Millisecond
)

var errGone = errors.New("session gone")

// sendNotification is a variable so tests never raise a desktop notification.
var sendNotification = notify.Send

type mode int

const (
	modeList mode = iota
	modeFocus
	modeNew // typing the name of a new worktree
	modeSearch
	modeRename
	modeNameAgent // typing the name of a new agent session (agent_actions.go)
	modeSwitch    // the view switcher overlay is open (switcher.go)
)

type row struct {
	org     string // header above the project ("Acme"), when shown
	project string // display name
	root    string // project root holding .bare
	wt      worktree.Worktree
	session string
	// running is whether any of the worktree's agents is; sess are the agents
	// themselves (agents.go).
	running bool
	sess    []agent
}

// size is the pane geometry a session was last resized to.
type size struct {
	session    string
	cols, rows int
}

type preview struct {
	session string
	cap     tmux.Capture
	ok      bool
	at      time.Time // when the capture was taken; older ones never replace newer
}

type Model struct {
	cfg config.Config
	// cfgPath is the file cfg was read from (reload.go); projectsFixed is set
	// when the project roots came from the command line, not from it.
	cfgPath       string
	projectsFixed bool
	tmux          tmux.Driver
	send          *sender
	wheel         *wheelBatch // wheel events waiting to be forwarded to a pane
	push          *pusher     // captures the shown pane when it prints (push.go)

	all    []row // every worktree; rows is the filtered view of it
	rows   []row
	errs   []string
	cursor int
	// selSession follows the selection across refreshes that reorder rows.
	selSession string
	// hdr is the id of the header the cursor is on, empty when it is on a
	// worktree (see tree.go); folds is the set of collapsed header ids.
	hdr   string
	folds map[string]bool

	// agents is the latest hook-reported state per session; prevStates is
	// what the last read showed, so only real transitions notify.
	agents       map[string]status.Entry
	prevStates   map[string]status.State
	statusLoaded bool

	mode          mode
	zoom          bool // the focused pane fills the terminal; see zoom.go
	help          bool
	confirmAll    bool
	confirmRemove string // session whose worktree removal awaits a second d
	status        string // errors and prompts
	notice        string // confirmations

	// New-worktree prompt state.
	aliases       map[string]string
	search        string
	attention     bool
	renameSession string
	// renameAgent is the agent session the rename prompt edits, "" when it
	// edits a worktree's label; nameFor and nameContinuing belong to the new
	// agent prompt.
	renameAgent    string
	nameFor        string
	nameContinuing bool
	// the view switcher: the choice in the overlay, and the mode to go back to.
	switchSel   int
	switchBack  mode
	moveSession string // worktree whose branch and folder the M prompt renames

	input   string
	newRoot string
	newBase string
	busy    bool

	width, height int
	preview       preview
	scroll        scrollState // scrollback of the pane being shown (scroll.go)
	sized         size
	// loop numbers the preview polling chain; ticks of an older loop are
	// dropped so a selection change never leaves two chains running.
	loop int
	// idlePolls counts captures in a row that showed no change; it drives
	// the back-off in poll.go.
	idlePolls int
	// git is the latest read of each worktree; gitSeeded is set once the
	// first full read has been requested.
	git       map[string]gitinfo.Status
	gitTicks  int
	gitSeeded bool
	gitBusy   bool           // a read is running; new requests queue in gitWant
	gitWant   map[string]row // worktrees asked for while a read was running
	gitAt     map[string]int // tick at which each worktree was last requested
	// ticks counts session refreshes, to run a full worktree scan every few.
	ticks int
	// vis is whether anybody can see drover; hidden pauses the polling.
	vis visibilityState
	// sub is the child row of the selected worktree the cursor is on
	// (viewAgent: the worktree row itself); the pane shown derives from it.
	// companions is the set of live companion sessions (companion_tree.go);
	// popup is lazygit's overlay (popup.go).
	sub paneView
	// subAgent is the agent the cursor is on when it is on an agent child
	// row, "" when it is on the worktree row (which stands for the primary
	// agent, see agents.go).
	subAgent   string
	companions map[string]bool
	popup      popupState
}

func New(cfg config.Config) Model {
	return Model{
		cfg: cfg, tmux: tmux.New(cfg.Socket), send: newSender(), push: &pusher{}, wheel: &wheelBatch{},
		gitWant: map[string]row{}, gitAt: map[string]int{}, vis: newVisibility(),
		companions: map[string]bool{},
	}
}

type (
	refreshed struct {
		rows       []row
		errs       []string
		aliases    map[string]string
		companions []string
	}
	tick            struct{}
	statusTick      struct{}
	statusMsg       struct{ entries map[string]status.Entry }
	worktreeCreated struct {
		root string
		wt   worktree.Worktree
		err  error
	}
	worktreeRemoved struct{ err error }
	attachDone      struct{ err error }
	previewTick     struct{ loop int }
	previewMsg      struct {
		loop    int
		session string
		cap     tmux.Capture
		err     error
		at      time.Time
	}
)

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), scheduleTick(), readStatus(), scheduleGitTick(), loadFolds(), m.probeNow())
}

func scheduleTick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tick{} })
}

func readStatus() tea.Cmd {
	return func() tea.Msg { return statusMsg{entries: status.ReadAll(status.Dir())} }
}

// refresh reads worktrees and live sessions off the UI goroutine.
func (m Model) refresh() tea.Cmd {
	cfg, driver := m.cfg, m.tmux
	return func() tea.Msg {
		live, companions, err := driver.ListSessions()
		var errs []string
		if err != nil {
			errs = append(errs, err.Error())
		}
		recs := session.Load(session.Path(status.Root()))
		var rows []row
		layout := layoutProjects(cfg.Projects)
		for _, root := range cfg.Projects {
			wts, err := worktree.List(root)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			info := layout[root]
			for _, wt := range wts {
				name := tmux.SessionName(info.unique, sessionKey(root, wt))
				rows = append(rows, row{
					org:     info.org,
					project: info.display,
					root:    root,
					wt:      wt,
					session: name,
				}.withAgents(buildAgents(name, wt.Branch, recs, live)))
			}
		}
		orderByOrg(rows)
		return refreshed{rows: rows, errs: errs, aliases: alias.Load(alias.Path(status.Root())), companions: companions}
	}
}

// sessionKey names a worktree by its path inside the project, which stays
// unique and readable even for detached worktrees.
func sessionKey(root string, wt worktree.Worktree) string {
	if rel, err := filepath.Rel(root, wt.Path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(wt.Path)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, done := m.gate(msg); done {
		return next, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.startPreview()
	case refreshed:
		m.all, m.errs, m.aliases = msg.rows, msg.errs, msg.aliases
		was, wasSession := m.sub, m.selSession
		m.setCompanions(msg.companions)
		m.applyFilter()
		if next, cmd := m.companionsChanged(was, wasSession); cmd != nil {
			return next, cmd
		}
		if !m.gitSeeded && len(m.all) > 0 {
			m.gitSeeded = true
			return m, m.requestGit(m.all)
		}
	case foldsLoaded:
		m.folds = msg.set
		m.reselect()
		return m, m.startPreview()
	case tick:
		m.ticks++
		if m.ticks%worktreeEvery == 0 {
			return m, tea.Batch(m.refresh(), scheduleTick())
		}
		return m, tea.Batch(m.refreshSessions(), scheduleTick())
	case sessionsMsg:
		was, wasSession := m.sub, m.selSession
		m = m.handleSessions(msg)
		return m.companionsChanged(was, wasSession)
	case gitTick:
		return m.handleGitTick()
	case gitMsg:
		return m.handleGit(msg)
	case statusTick:
		return m, readStatus()
	case statusMsg:
		return m.handleStatus(msg)
	case configEdited:
		return m.handleConfigEdited(msg)
	case attachDone:
		if msg.err != nil {
			m.status = "attach: " + msg.err.Error()
		}
		m.sized = size{}
		return m, tea.Batch(m.refresh(), m.startPreview(), m.readSelectedGit())
	case previewTick:
		if msg.loop != m.loop {
			return m, nil
		}
		return m, m.pollPreview()
	case previewMsg:
		return m.handlePreview(msg)
	case scrollMsg:
		return m.handleScroll(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case echoMsg:
		if m.takeCapture(msg.session, msg.cap, msg.at) {
			return m, nil
		}
		if r, ok := m.selected(); ok && m.target(r) == msg.session && !msg.at.Before(m.preview.at) {
			if !m.preview.ok || !sameCapture(m.preview.cap, msg.cap) {
				m.idlePolls = 0
			}
			m.preview = preview{session: msg.session, cap: msg.cap, ok: true, at: msg.at}
		}
		return m, nil
	case worktreeCreated:
		return m.handleCreated(msg)
	case branchMoved:
		return m.handleMoved(msg)
	case worktreeRemoved:
		m.busy = false
		if msg.err != nil {
			m.status = msg.err.Error()
		} else {
			m.notice = "worktree removed (the branch is kept)"
		}
		return m, m.refresh()
	case tea.KeyPressMsg:
		switch m.mode {
		case modeFocus:
			return m.handleFocusKey(msg)
		case modePopup:
			return m.handlePopupKey(msg)
		case modeNew:
			return m.handleNewKey(msg)
		case modeSearch:
			return m.handleSearchKey(msg)
		case modeRename:
			return m.handleRenameKey(msg)
		case modeNameAgent:
			return m.handleNameAgentKey(msg)
		case modeMoveBranch:
			return m.handleMoveKey(msg)
		case modeSwitch:
			return m.handleSwitchKey(msg)
		}
		return m.handleKey(msg)
	case tea.PasteMsg:
		return m.handlePaste(msg)
	}
	return m, nil
}

// handlePaste sends a bracketed paste where the key presses of the current
// mode would go: to the pane in focus mode and the popup, into the field of a
// prompt, and nowhere in the list.
func (m Model) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFocus:
		if r, ok := m.selected(); ok && m.targetRunning(r) {
			m.scroll = scrollState{} // as for any key: never type into a view that is not live
			m.forwardPaste(msg.Content, m.target(r))
			if m.idlePolls > 1 { // the agent is about to react: leave slow polling at once
				return m, m.startPreview()
			}
		}
	case modePopup:
		m.forwardPaste(msg.Content, m.popup.session)
		if m.idlePolls > 1 {
			return m, m.startPreview()
		}
	case modeNew:
		return m.handleNewKey(pasteAsKey(msg.Content))
	case modeSearch:
		return m.handleSearchKey(pasteAsKey(msg.Content))
	case modeRename:
		return m.handleRenameKey(pasteAsKey(msg.Content))
	case modeNameAgent:
		return m.handleNameAgentKey(pasteAsKey(msg.Content))
	case modeMoveBranch:
		return m.handleMoveKey(pasteAsKey(msg.Content))
	}
	return m, nil
}

// handleStatus stores new hook states and notifies on transitions into a
// state that needs the user. The first read only seeds the baseline, so
// starting drover never replays old states as notifications.
func (m Model) handleStatus(msg statusMsg) (tea.Model, tea.Cmd) {
	next := make(map[string]status.State, len(msg.entries))
	var cmds []tea.Cmd
	for session, e := range msg.entries {
		next[session] = e.State
		// An agent that just stopped working has just changed files.
		if m.statusLoaded && m.prevStates[session] == status.Working && e.State != status.Working {
			if r, ok := m.rowFor(session); ok {
				if c := m.requestGit([]row{r}); c != nil {
					cmds = append(cmds, c)
				}
			}
		}
		if !m.statusLoaded || !m.cfg.Notify || m.prevStates[session] == e.State {
			continue
		}
		if m.looking(session) {
			continue // you are looking at it (not when a companion is what you see)
		}
		if body := notification(m.notifyName(session), e.State); body != "" {
			cmds = append(cmds, func() tea.Msg { _ = sendNotification("drover", body); return nil })
		}
	}
	m.agents, m.prevStates, m.statusLoaded = msg.entries, next, true
	if m.attention {
		m.applyFilter() // the attention filter follows live states
	}
	cmds = append(cmds, tea.Tick(m.statusEvery(), func(time.Time) tea.Msg { return statusTick{} }))
	return m, tea.Batch(cmds...)
}

func notification(session string, s status.State) string {
	switch s {
	case status.Waiting:
		return session + " needs you"
	case status.Done:
		return session + " finished"
	case status.Error:
		return session + " hit an error"
	}
	return ""
}

func (m Model) handlePreview(msg previewMsg) (tea.Model, tea.Cmd) {
	if msg.loop != m.loop {
		return m, nil
	}
	if msg.err != nil {
		m.preview = preview{}
		if errors.Is(msg.err, errGone) {
			if next, cmd, ok := m.companionGone(msg.session); ok {
				return next, cmd
			}
			if m.mode == modeFocus {
				m.mode = modeList
				m.status = "session ended"
			}
			m.sized = size{}
			return m, tea.Batch(m.refresh(), m.nextPoll())
		}
		return m, m.nextPoll()
	}
	if m.takeCapture(msg.session, msg.cap, msg.at) {
		return m, m.nextPoll()
	}
	if r, ok := m.selected(); ok && m.target(r) == msg.session && !msg.at.Before(m.preview.at) {
		if m.preview.ok && m.preview.session == msg.session && sameCapture(m.preview.cap, msg.cap) {
			m.idlePolls++
		} else {
			m.idlePolls = 0
		}
		m.preview = preview{session: msg.session, cap: msg.cap, ok: true, at: msg.at}
	}
	return m, m.nextPoll()
}

// startPreview restarts the polling chain with an immediate capture.
func (m *Model) startPreview() tea.Cmd {
	m.loop++
	m.idlePolls = 0
	return m.pollPreview()
}

func (m Model) nextPoll() tea.Cmd {
	loop, d := m.loop, pollInterval(m.mode == modeFocus || m.mode == modePopup, m.idlePolls)
	return tea.Tick(d, func(time.Time) tea.Msg { return previewTick{loop: loop} })
}

// pollPreview captures the selected session's pane, resizing it first when
// the right-hand pane changed shape.
func (m *Model) pollPreview() tea.Cmd {
	if m.popup.open {
		return m.pollPopup()
	}
	r, ok := m.selected()
	if !ok || !m.targetRunning(r) || m.width == 0 {
		m.push.follow(m.tmux, "")
		return m.nextPoll()
	}
	shown := m.target(r)
	m.push.follow(m.tmux, m.pushTarget(r))
	var cmds []tea.Cmd
	if cols, rows := m.paneSize(); cols > 0 && rows > 0 {
		if want := (size{shown, cols, rows}); m.sized != want {
			m.sized = want
			driver := m.tmux
			cmds = append(cmds, func() tea.Msg {
				_ = driver.Resize(shown, cols, rows)
				return nil
			})
		}
	}
	if c, ok := m.pollScroll(); ok { // scrolled back: read that window, not the live screen
		return tea.Batch(append(cmds, c)...)
	}
	loop, driver, name := m.loop, m.tmux, shown
	cmds = append(cmds, func() tea.Msg {
		at := time.Now()
		c, err := driver.Capture(name)
		if err != nil && !driver.Has(name) {
			err = errGone
		}
		return previewMsg{loop: loop, session: name, cap: c, err: err, at: at}
	})
	return tea.Batch(cmds...)
}

func (m Model) selected() (row, bool) {
	if m.hdr != "" {
		return row{}, false // the cursor is on a header
	}
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

func (m *Model) move(delta int) {
	m.scroll = scrollState{} // scrollback belongs to the pane you leave
	m.step(delta)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	m.status, m.notice = "", ""
	if key != "X" {
		m.confirmAll = false
	}
	if key != "d" {
		m.confirmRemove = ""
	}
	if m.help {
		if key == "?" || key == "esc" || key == "q" {
			m.help = false
		}
		return m, nil
	}
	if c, ok := m.scrollListKey(msg); ok {
		return m, c
	}
	if step, jump, ok := m.switchKeyPressed(key); ok {
		return m.switchView(step, jump)
	}
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help = true
	case "up", "k":
		m.move(-1)
		return m, tea.Batch(m.startPreview(), m.readSelectedGit())
	case "down", "j":
		m.move(1)
		return m, tea.Batch(m.startPreview(), m.readSelectedGit())
	case "left", "h":
		return m.collapse()
	case "right", "l":
		return m.expand()
	case "F":
		return m.foldAll()
	case "enter":
		if m.hdr != "" {
			return m.toggleHeader()
		}
		return m.focus()
	case "A":
		return m.attach()
	case "a":
		return m.openView(viewAgent)
	case "T":
		return m.openView(viewTerminal)
	case "o":
		return m.openView(viewEditor)
	case "G":
		return m.openPopup()
	case "c":
		return m.openConfig()
	case "R":
		return m.reloadConfig()
	case "n":
		return m.openNew()
	case "N":
		return m.newAgent()
	case "/":
		return m.openSearch()
	case "w":
		return m.toggleAttention()
	case "r":
		return m.openRename()
	case "M":
		return m.openMove()
	case "esc":
		if m.filtering() {
			return m.clearFilters()
		}
	case "d":
		return m.remove()
	case "v":
		return m.resume()
	case "x":
		return m.kill()
	case "X":
		return m.killAll()
	}
	return m, nil
}

func (m Model) focus() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	if v := m.viewOf(r); v != viewAgent {
		return m.openView(v) // restarts the companion if it has ended
	}
	a, has := m.curAgent(r)
	switch {
	case !has:
		return m.openNameAgent(r, false) // no session here yet: ask for a name
	case !a.running:
		return m.revive(r, a)
	}
	m.enterFocus()
	return m, m.startPreview()
}

func (m Model) attach() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	// Attaches whatever the pane shows, companion included.
	if v := m.viewOf(r); v != viewAgent {
		if !m.ensureCompanion(r, v) {
			return m, nil
		}
	} else {
		a, has := m.curAgent(r)
		if !has {
			m.status = "no session here: enter starts one"
			return m, nil
		}
		if !m.reviveSession(r, a) {
			return m, nil
		}
	}
	name := m.target(r)
	_ = m.tmux.PrepareAttach(name)
	return m, tea.ExecProcess(m.tmux.AttachCmd(name), func(err error) tea.Msg {
		return attachDone{err: err}
	})
}

// kill is x on the row under the cursor: a companion is closed; a running
// agent is stopped but keeps its row, so it can be resumed; a stopped one is
// forgotten.
func (m Model) kill() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if ok && m.viewOf(r) != viewAgent {
		return m.closeCompanion(r)
	}
	if !ok {
		return m, nil
	}
	a, has := m.curAgent(r)
	if !has {
		return m, nil
	}
	if _, onChild := m.agentUnderCursor(r); !onChild && runningCount(r) > 1 {
		m.status = "several agents run here: move to one (↓) and press x"
		return m, nil
	}
	if !a.running {
		return m.forgetAgent(r, a)
	}
	if err := m.tmux.Kill(a.session); err != nil {
		m.status = err.Error()
	}
	_ = status.Remove(status.Dir(), a.session)
	m.sized = size{}
	m.preview = preview{}
	m.scroll = scrollState{}
	m.setRunning(a.session, false)
	return m, m.refresh()
}

// runningCount is how many of a worktree's agents are running.
func runningCount(r row) int {
	n := 0
	for _, a := range r.agentList() {
		if a.running {
			n++
		}
	}
	return n
}

func (m Model) killAll() (tea.Model, tea.Cmd) {
	if !m.confirmAll {
		m.confirmAll = true
		m.status = "press X again to kill every session, terminal, editor and lazygit"
		return m, nil
	}
	m.confirmAll = false
	if err := m.tmux.KillServer(); err != nil {
		m.status = err.Error()
	}
	for _, r := range m.all {
		_ = status.Remove(status.Dir(), r.session)
		for _, a := range r.agentList() {
			_ = status.Remove(status.Dir(), a.session)
		}
	}
	m.forgetCompanions() // the server took them down too
	m.sized = size{}
	m.preview = preview{}
	return m, m.refresh()
}

// handleFocusKey forwards everything to the agent except the way back.
func (m Model) handleFocusKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if s := msg.String(); s == `ctrl+\` || s == "ctrl+q" {
		m.mode = modeList
		// The agent may have changed files while you were typing to it.
		return m, tea.Batch(m.startPreview(), m.readSelectedGit())
	}
	if step, jump, ok := m.switchKeyPressed(msg.String()); ok {
		return m.switchView(step, jump)
	}
	r, ok := m.selected()
	if !ok || !m.targetRunning(r) {
		m.mode = modeList
		return m, nil
	}
	if msg.String() == m.zoomKey() {
		return m.toggleZoom()
	}
	if c, ok := m.scrollFocusKey(msg); ok {
		return m, c
	}
	m.forward(msg, m.target(r))
	// The agent is about to react (echo, then maybe stream an answer): leave
	// slow polling at once instead of waiting out a long interval.
	if m.idlePolls > 1 {
		return m, m.startPreview()
	}
	return m, nil
}

package ui

import (
	"slices"
	"time"

	"github.com/lnware/drover/internal/session"
	"github.com/lnware/drover/internal/status"
)

// A worktree can run several agents (see package session). Each one is an
// agent here: its own tmux session, status file and conversation. The worktree
// row keeps what is about the place (git, folds, companions, its label), and
// m.sub/m.subAgent say which agent the cursor is on.

// agent is one agent session of a worktree.
type agent struct {
	session string // the tmux session name
	id      string // "" for a session started before sessions were recorded
	name    string
	claude  string // the Claude conversation id, "" when unknown
	running bool
	created time.Time
}

// agentList is the row's agents, oldest first. A row built without them (as
// the tests do) that is running has the one session an earlier drover would
// have started, named after the worktree.
func (r row) agentList() []agent {
	if len(r.sess) > 0 {
		return r.sess
	}
	if r.running {
		return []agent{{session: r.session, name: session.DefaultName(r.wt.Branch, nil), running: true}}
	}
	return nil
}

// agentItemID is the item id of an agent's child row. A session started by an
// earlier drover is named exactly like its worktree, so the id needs a prefix
// of its own to stay distinct from the worktree row's (and from fold ids,
// "o:", "p:" and "g:", and companion ids, which contain an "@").
func agentItemID(agentSession string) string { return "a:" + agentSession }

// primaryAgent is the agent a worktree row stands for: the first one that is
// running, else the first. It is what the pane shows while the cursor is on
// the worktree itself, so moving down the list still shows each worktree's
// agent, as it did when a worktree had only one.
func primaryAgent(list []agent) (agent, bool) {
	if i := slices.IndexFunc(list, func(a agent) bool { return a.running }); i >= 0 {
		return list[i], true
	}
	if len(list) > 0 {
		return list[0], true
	}
	return agent{}, false
}

// curAgent is the agent the cursor is on for a worktree: the child row it is
// on, else the worktree's primary agent.
func (m Model) curAgent(r row) (agent, bool) {
	list := r.agentList()
	if m.subAgent != "" {
		if sel, ok := m.selected(); ok && sel.session == r.session {
			if i := slices.IndexFunc(list, func(a agent) bool { return a.session == m.subAgent }); i >= 0 {
				return list[i], true
			}
		}
	}
	return primaryAgent(list)
}

// buildAgents makes the agent list of the worktree base: a record for each
// session drover knows, then any live session of the worktree that has none
// (an earlier drover's, or one whose record was lost).
func buildAgents(base, branch string, recs []session.Session, live []string) []agent {
	var out []agent
	have := map[string]bool{}
	for _, rec := range session.ForWorktree(recs, base) {
		name := session.TmuxName(base, rec.ID)
		out = append(out, agent{
			session: name, id: rec.ID, name: rec.Name, claude: rec.Claude,
			created: rec.Created, running: slices.Contains(live, name),
		})
		have[name] = true
	}
	for _, s := range live {
		wt, id, recorded := session.Parse(s)
		if wt != base || have[s] {
			continue
		}
		a := agent{session: s, id: id, running: true}
		if recorded {
			a.name = id // all that is known of a session whose record is gone
		}
		out = append(out, a)
	}
	// Sessions without a record come first (they are the oldest), and a legacy
	// one is named after the branch, as a new session would be.
	slices.SortStableFunc(out, func(a, b agent) int { return a.created.Compare(b.created) })
	var taken []string
	for _, a := range out {
		if a.name != "" {
			taken = append(taken, a.name)
		}
	}
	for i := range out {
		if out[i].name == "" {
			out[i].name = session.DefaultName(branch, taken)
			taken = append(taken, out[i].name)
		}
	}
	return out
}

// anyRunning reports whether any of the agents is running.
func anyRunning(list []agent) bool {
	return slices.ContainsFunc(list, func(a agent) bool { return a.running })
}

// withAgents sets a row's agent list and the running flag derived from it.
func (r row) withAgents(list []agent) row {
	r.sess = list
	r.running = anyRunning(list)
	return r
}

// urgency orders what a row's agents say, most urgent first, so a worktree
// shows its most urgent agent.
func urgency(s status.State, hasData bool) int {
	if !hasData {
		return 1
	}
	switch s {
	case status.Waiting:
		return 5
	case status.Error:
		return 4
	case status.Done:
		return 3
	case status.Working:
		return 2
	}
	return 1
}

// agentState is one agent's hook-reported state, and whether it has any.
func (m Model) agentState(a agent) (status.State, bool) {
	e, ok := m.agents[a.session]
	return e.State, ok
}

// worktreeState is the state a worktree shows: its most urgent running
// agent's. ok is false when no agent is running or none has reported.
func (m Model) worktreeState(r row) (status.State, bool) {
	best, bestU, found := status.State(""), 0, false
	for _, a := range r.agentList() {
		if !a.running {
			continue
		}
		s, ok := m.agentState(a)
		if !ok {
			continue
		}
		if u := urgency(s, true); u > bestU {
			best, bestU, found = s, u, true
		}
	}
	return best, found
}

// agentDisplay is how one agent looks in the list and the pane header. A
// running session with no hook data (started elsewhere, or hooks not
// installed) is shown as plain "running" rather than guessed at.
func (m Model) agentDisplay(a agent) display {
	if !a.running {
		return display{glyph: "○", style: idleStyle}
	}
	s, ok := m.agentState(a)
	if !ok {
		return display{glyph: "●", word: "running", style: dimStyle}
	}
	return stateDisplay(s)
}

func stateDisplay(s status.State) display {
	switch s {
	case status.Working:
		return display{"●", "working", workingStyle}
	case status.Waiting:
		return display{"◆", "waiting", waitingStyle}
	case status.Done:
		return display{"✓", "done", doneStyle}
	case status.Error:
		return display{"✗", "error", errStyle}
	}
	return display{"●", "idle", runningStyle}
}

package ui

// A repeatable lag harness: it runs the real Model inside a real Bubble Tea
// program (output captured, no terminal) against real tmux, with Helix,
// lazygit and heavy shell output as the pane's program, and measures what
// the user feels:
//
//   - keypress-to-screen latency: from p.Send(key) to the first rendered
//     frame that shows its effect;
//   - frames per second drover actually renders during sustained output or
//     scrolling, counted as writes the Bubble Tea renderer makes.
//
// It is slow and machine-dependent, so it only runs on request:
//
//	DROVER_LAGBENCH=1 go test ./internal/ui -run TestLagBench -v -count=1
//
// DROVER_LAGBENCH_VIEW=term|edit|git runs the scenario in a companion row
// (T, o) or the lazygit popup (G) instead of as the agent: the same capture
// path, plus the companion keys. With git the program draws inside the popup's
// smaller area and the frame is spliced, so it also covers the overlay.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lnware/drover/internal/config"
)

type chunk struct {
	at   time.Time
	data string
}

// sink records every write the renderer makes, with its time.
type sink struct {
	mu     sync.Mutex
	chunks []chunk
}

func (s *sink) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.chunks = append(s.chunks, chunk{time.Now(), string(p)})
	s.mu.Unlock()
	return len(p), nil
}

func (s *sink) since(t time.Time) []chunk {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []chunk
	for _, c := range s.chunks {
		if c.at.After(t) {
			out = append(out, c)
		}
	}
	return out
}

// text is the visible text of everything rendered since t.
func (s *sink) text(t time.Time) string {
	var b strings.Builder
	for _, c := range s.since(t) {
		b.WriteString(ansi.Strip(c.data))
	}
	return b.String()
}

type scenario struct {
	name  string
	cmd   string
	ready string // visible text that shows the program is up
	// typed: keys are letters and the match is their cumulative echo;
	// otherwise a key is `key` and any rendered change counts.
	typed  bool
	prefix string // key sent once before typing (helix insert mode)
	key    string
	// hold sends key at holdHz during the fps phase; zero means the program
	// produces the output by itself.
	hold bool
	// skipLatency / skipFPS drop the half of the measurement that makes no
	// sense: a stream has no keypress to time, a typing test has no output.
	skipLatency, skipFPS bool
	needHx               bool
	needLg               bool
}

func benchScenarios() []scenario {
	return []scenario{
		{name: "shell-typing", cmd: "env PS1='bench$ ' sh", ready: "bench$", typed: true, skipFPS: true},
		{name: "shell-flood", cmd: "seq 1 40000000", ready: "00", skipLatency: true},
		{name: "shell-stream", cmd: `sh -c 'i=0; while :; do echo "line $i"; i=$((i+1)); sleep 0.01; done'`, ready: "line ", skipLatency: true},
		{name: "fullscreen-redraw", cmd: `sh -c 'i=0; while :; do printf "\033[H"; n=0; while [ $n -lt 40 ]; do echo "frame $i row $n ..............."; n=$((n+1)); done; i=$((i+1)); sleep 0.02; done'`, ready: "frame ", skipLatency: true},
		{name: "helix-typing", cmd: "hx -c {cfg}/hx.toml {wt}/new.txt", ready: "new.txt", typed: true, prefix: "i", skipFPS: true, needHx: true},
		{name: "helix-scroll", cmd: "hx -c {cfg}/hx.toml {wt}/big.txt", ready: "big.txt", key: "j", hold: true, needHx: true},
		{name: "lazygit-scroll", cmd: "lazygit", ready: "Status", key: "j", hold: true, needLg: true},
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestLagBench(t *testing.T) {
	if os.Getenv("DROVER_LAGBENCH") == "" {
		t.Skip("set DROVER_LAGBENCH=1 to run the lag harness")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	only := os.Getenv("DROVER_LAGBENCH_ONLY")
	for _, sc := range benchScenarios() {
		if only != "" && !strings.Contains(sc.name, only) {
			continue
		}
		if sc.needHx {
			if _, err := exec.LookPath("hx"); err != nil {
				t.Logf("%s: hx not installed, skipped", sc.name)
				continue
			}
		}
		if sc.needLg {
			if _, err := exec.LookPath("lazygit"); err != nil {
				t.Logf("%s: lazygit not installed, skipped", sc.name)
				continue
			}
		}
		t.Run(sc.name, func(t *testing.T) { runBench(t, sc) })
	}
}

func runBench(t *testing.T, sc scenario) {
	state, cfgDir, root := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("DROVER_STATE_DIR", state)
	// Keep Helix and lazygit away from the user's own config and state.
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_STATE_HOME", cfgDir)
	t.Setenv("XDG_CACHE_HOME", cfgDir)
	t.Setenv("XDG_DATA_HOME", cfgDir)
	_ = os.WriteFile(filepath.Join(cfgDir, "hx.toml"), []byte(""), 0o644)
	// lazygit opens a welcome popup on first run, which swallows keys.
	_ = os.MkdirAll(filepath.Join(cfgDir, "lazygit"), 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "lazygit", "config.yml"), []byte("disableStartupPopups: true\n"), 0o644)

	seed := t.TempDir()
	gitIn(t, seed, "init", "-q")
	gitIn(t, seed, "commit", "-q", "--allow-empty", "-m", "initial")
	gitIn(t, root, "clone", "-q", "--bare", seed, filepath.Join(root, ".bare"))
	_ = os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ./.bare\n"), 0o644)
	gitIn(t, root, "--git-dir=.bare", "worktree", "add", "-q", "main", "main")
	wt := filepath.Join(root, "main")
	var big strings.Builder
	for i := 0; i < 100000; i++ {
		fmt.Fprintf(&big, "line %d of the big file\n", i)
	}
	_ = os.WriteFile(filepath.Join(wt, "big.txt"), []byte(big.String()), 0o644)
	_ = os.MkdirAll(filepath.Join(wt, "files"), 0o755)
	for i := 0; i < 300; i++ {
		_ = os.WriteFile(filepath.Join(wt, "files", fmt.Sprintf("f%03d.txt", i)), []byte("x\n"), 0o644)
	}

	cmd := strings.NewReplacer("{cfg}", cfgDir, "{wt}", wt).Replace(sc.cmd)
	cfg := config.Default()
	cfg.Projects = []string{root}
	cfg.Socket = fmt.Sprintf("agt-companion-bench-%d", os.Getpid())
	view := os.Getenv("DROVER_LAGBENCH_VIEW")
	switch view {
	case "term":
		cfg.TerminalCmd = cmd
	case "edit":
		cfg.EditorCmd = cmd
	case "git":
		cfg.LazygitCmd = cmd
	default:
		cfg.AgentCmd = cmd
	}
	m := New(cfg)
	t.Cleanup(func() { _ = m.tmux.KillServer() })
	out := &sink{}
	p := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(out), tea.WithoutSignalHandler(), tea.WithFPS(120))
	m.Notify(p.Send)
	if os.Getenv("DROVER_LAGBENCH_NOPUSH") != "" {
		m.push.notify = nil // poll-only, as before push.go
	}
	done := make(chan struct{})
	go func() { _, _ = p.Run(); close(done) }()
	t.Cleanup(func() { p.Quit(); <-done })

	waitFor := func(what string, since time.Time, d time.Duration) {
		t.Helper()
		for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if strings.Contains(out.text(since), what) {
				return
			}
		}
		t.Fatalf("timed out waiting for %q; last screen: %.600q", what, out.text(time.Now().Add(-time.Second)))
	}

	start := time.Now()
	p.Send(tea.WindowSizeMsg{Width: 200, Height: 50})
	waitFor("main", start, 10*time.Second)
	start = time.Now()
	switch view {
	case "term":
		p.Send(keyRunes("T"))
	case "edit":
		p.Send(keyRunes("o"))
	case "git":
		p.Send(keyRunes("G"))
	default:
		p.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	waitFor(sc.ready, start, 15*time.Second)
	time.Sleep(700 * time.Millisecond) // let startup output settle

	// Latency.
	var lat []time.Duration
	if sc.prefix != "" {
		p.Send(keyRunes(sc.prefix))
		time.Sleep(300 * time.Millisecond)
	}
	typedSoFar := ""
	letters := "qwertyuiopasdfghjklzxcvbnm"
	for i := 0; i < 40 && !sc.skipLatency; i++ {
		var key, want string
		if sc.typed {
			key = string(letters[i%len(letters)])
			typedSoFar += key
			want = typedSoFar
		} else {
			key = sc.key
		}
		t0 := time.Now()
		p.Send(keyRunes(key))
		var got time.Time
		for end := t0.Add(2 * time.Second); time.Now().Before(end); time.Sleep(time.Millisecond) {
			chunks := out.since(t0)
			if sc.typed {
				var b strings.Builder
				for _, c := range chunks {
					b.WriteString(ansi.Strip(c.data))
				}
				if strings.Contains(b.String(), want) && len(chunks) > 0 {
					got = chunks[len(chunks)-1].at
					// the first chunk that completes the match is the one that matters
					b.Reset()
					for _, c := range chunks {
						b.WriteString(ansi.Strip(c.data))
						if strings.Contains(b.String(), want) {
							got = c.at
							break
						}
					}
					break
				}
			} else if len(chunks) > 0 {
				got = chunks[0].at
				break
			}
		}
		if got.IsZero() {
			if i == 0 {
				t.Logf("no echo for %q within 2s; rendered since: %.400q; last frames: %.1500q", key, out.text(t0), out.text(start))
			}
			lat = append(lat, 2*time.Second)
		} else {
			lat = append(lat, got.Sub(t0))
		}
		time.Sleep(150 * time.Millisecond)
	}
	slices.Sort(lat)
	if len(lat) == 0 {
		lat = []time.Duration{0}
	}
	pct := func(q float64) time.Duration { return lat[min(int(q*float64(len(lat))), len(lat)-1)] }

	// Frame rate.
	time.Sleep(500 * time.Millisecond)
	const window = 5 * time.Second
	t0 := time.Now()
	if sc.skipFPS {
		t.Logf("RESULT %-18s latency median %4.0f ms  p90 %4.0f ms  max %4.0f ms", sc.name, ms(pct(0.5)), ms(pct(0.9)), ms(lat[len(lat)-1]))
		return
	}
	if sc.hold {
		for tk := time.NewTicker(33 * time.Millisecond); time.Since(t0) < window; <-tk.C {
			p.Send(keyRunes(sc.key))
		}
	} else {
		time.Sleep(window)
	}
	chunks := out.since(t0)
	var times []float64
	for _, c := range chunks {
		times = append(times, c.at.Sub(t0).Seconds())
	}
	sort.Float64s(times)
	lats := "latency n/a"
	if !sc.skipLatency {
		lats = fmt.Sprintf("latency median %4.0f ms  p90 %4.0f ms  max %4.0f ms", ms(pct(0.5)), ms(pct(0.9)), ms(lat[len(lat)-1]))
	}
	t.Logf("RESULT %-18s %s | %5.1f frames/s (%d frames in %.0fs)",
		sc.name, lats, float64(len(chunks))/window.Seconds(), len(chunks), window.Seconds())
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

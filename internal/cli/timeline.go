package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/config"
	"github.com/rwrife/back-then/internal/sessions"
	"github.com/rwrife/back-then/internal/store"
	"github.com/rwrife/back-then/internal/timeline"
)

// defaultTimelineWidth is the fallback number of axis columns when the terminal
// width cannot be determined (e.g. non-TTY or before the first resize message).
const defaultTimelineWidth = 60

// newTimelineCmd returns the `back-then timeline` subcommand: an interactive TUI
// scrubber. A horizontal time axis spans the whole index; arrow keys move the
// cursor across it and sessions light up beneath. Selecting a column lists its
// files in a side pane; Enter copies the highlighted file's path and (with o)
// opens its containing folder. The index is read-only throughout.
func newTimelineCmd(dbPath *string, cfg config.Config) *cobra.Command {
	var gap time.Duration

	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "Scrub across a time axis and watch sessions light up (interactive TUI)",
		Long: `Open an interactive time scrubber over the whole index.

A horizontal axis spans from your oldest indexed file to your newest. The
"heat" under each column shows how many sessions landed in that slice of time.
Move the cursor with the arrow keys (or h/l); press Tab / Shift+Tab to jump
straight to the next/previous populated slice. The sessions under the cursor
light up and their files appear in the side pane.

Keys:
  ` + "\u2190/\u2192" + ` or h/l   move the cursor one column
  Tab / Shift+Tab   jump to next / previous populated column
  ` + "\u2191/\u2193" + ` or j/k   move the file selection in the side pane
  Home / End        jump to the oldest / newest column
  enter / c         copy the selected file's path to the clipboard
  o                 open the selected file's containing folder
  q / esc           quit

The index is opened read-only; timeline never modifies your files or the
database.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveDBPath(*dbPath, cfg.DB)
			if err != nil {
				return fmt.Errorf("resolve index path: %w", err)
			}

			st, err := store.Open(path)
			if err != nil {
				return err
			}
			defer st.Close()

			files, err := st.AllFiles()
			if err != nil {
				return err
			}
			if len(files) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(),
					"Index is empty. Run `back-then index <path>` to populate it.")
				return err
			}

			sess := sessions.Cluster(files, sessions.Options{Gap: gap, FolderAware: true})

			labelByID := map[string]string{}
			labels, err := st.Labels()
			if err != nil {
				return err
			}
			for _, l := range labels {
				labelByID[l.ID] = l.Label
			}

			m := newTimelineModel(sess, labelByID)
			p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(cmd.OutOrStderr()))
			_, err = p.Run()
			return err
		},
	}

	cmd.Flags().DurationVar(&gap, "gap", effectiveGap(cfg, sessions.DefaultGap),
		"gap between files that starts a new session (e.g. 90m, 3h)")

	return cmd
}

// timelineModel is the bubbletea model wrapping the pure timeline.Model with the
// side-pane file selection, terminal size, and a transient status line.
type timelineModel struct {
	sessions  []sessions.Session
	labelByID map[string]string

	tl     timeline.Model
	width  int
	height int

	// fileCursor selects a file within the flattened list of the current
	// column's sessions' files.
	fileCursor int
	status     string
}

func newTimelineModel(sess []sessions.Session, labelByID map[string]string) timelineModel {
	w := defaultTimelineWidth
	return timelineModel{
		sessions:  sess,
		labelByID: labelByID,
		tl:        timeline.New(sess, w),
		width:     0,
		height:    0,
	}
}

func (m timelineModel) Init() tea.Cmd { return nil }

// axisWidth derives the number of axis columns from the terminal width, leaving
// room for a left margin. It never drops below a small floor so the axis stays
// usable on narrow terminals.
func axisWidth(termWidth int) int {
	if termWidth <= 0 {
		return defaultTimelineWidth
	}
	w := termWidth - 4
	if w < 10 {
		w = 10
	}
	return w
}

func (m timelineModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Rebuild the axis to fit the new width, preserving roughly where the
		// cursor pointed by re-centering on the newest populated column.
		m.tl = timeline.New(m.sessions, axisWidth(msg.Width))
		m.fileCursor = 0
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "left", "h":
			if m.tl.Left() {
				m.fileCursor = 0
				m.status = ""
			}
		case "right", "l":
			if m.tl.Right() {
				m.fileCursor = 0
				m.status = ""
			}
		case "tab":
			if m.tl.NextSession() {
				m.fileCursor = 0
				m.status = ""
			}
		case "shift+tab":
			if m.tl.PrevSession() {
				m.fileCursor = 0
				m.status = ""
			}
		case "home":
			m.tl.Home()
			m.fileCursor = 0
			m.status = ""
		case "end":
			m.tl.JumpEnd()
			m.fileCursor = 0
			m.status = ""
		case "up", "k":
			if m.fileCursor > 0 {
				m.fileCursor--
			}
		case "down", "j":
			if m.fileCursor < len(m.currentFiles())-1 {
				m.fileCursor++
			}
		case "enter", "c":
			if f, ok := m.selectedFile(); ok {
				if err := copyToClipboard(f.Path); err != nil {
					m.status = "copy path: " + err.Error() + " (path: " + f.Path + ")"
				} else {
					m.status = "copied path: " + f.Path
				}
			}
		case "o":
			if f, ok := m.selectedFile(); ok {
				if err := openFolder(f.ParentDir); err != nil {
					m.status = "open folder: " + err.Error()
				} else {
					m.status = "opened folder: " + f.ParentDir
				}
			}
		}
	}
	return m, nil
}

// currentFiles is the flattened, chronological list of files across every
// session in the cursor's column.
func (m timelineModel) currentFiles() []store.FileRecord {
	var out []store.FileRecord
	for _, s := range m.tl.CurrentSessions() {
		out = append(out, s.Files...)
	}
	return out
}

// selectedFile returns the file under the side-pane cursor, if any.
func (m timelineModel) selectedFile() (store.FileRecord, bool) {
	files := m.currentFiles()
	if m.fileCursor < 0 || m.fileCursor >= len(files) {
		return store.FileRecord{}, false
	}
	return files[m.fileCursor], true
}

var (
	styleAxisDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleAxisWarm  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleAxisHot   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	styleCursor    = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	styleHeading   = lipgloss.NewStyle().Bold(true)
	styleSelected  = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	styleFaint     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleStatusMsg = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
)

// heatGlyph maps a column's heat (scaled to the busiest column) to a bar glyph.
func heatGlyph(heat, max int) (string, lipgloss.Style) {
	if heat <= 0 {
		return "\u00b7", styleAxisDim // middle dot for empty slices
	}
	// Choose a glyph by relative intensity.
	ratio := float64(heat) / float64(max)
	switch {
	case ratio > 0.66:
		return "\u2588", styleAxisHot // full block
	case ratio > 0.33:
		return "\u2593", styleAxisWarm // dark shade
	default:
		return "\u2592", styleAxisWarm // medium shade
	}
}

func (m timelineModel) View() string {
	var b strings.Builder

	title := styleHeading.Render("back-then timeline")
	span := ""
	if !m.tl.Start.IsZero() {
		span = styleFaint.Render(fmt.Sprintf("  %s  \u2192  %s",
			m.tl.Start.Format("2006-01-02"), m.tl.End.Format("2006-01-02")))
	}
	b.WriteString(title + span + "\n\n")

	// Axis row.
	max := m.tl.MaxHeat()
	b.WriteString("  ")
	for i, c := range m.tl.Columns {
		glyph, style := heatGlyph(c.Heat(), max)
		if i == m.tl.Cursor {
			b.WriteString(styleCursor.Render("\u2502")) // cursor marker under column
		} else {
			b.WriteString(style.Render(glyph))
		}
	}
	b.WriteString("\n")

	// Cursor time readout.
	col := m.tl.CurrentColumn()
	when := "\u2014"
	if !col.Start.IsZero() {
		when = col.Start.Format("2006-01-02 15:04")
	}
	b.WriteString("  " + styleFaint.Render("cursor: "+when) + "\n\n")

	// Sessions + files pane for the current column.
	cur := m.tl.CurrentSessions()
	if len(cur) == 0 {
		b.WriteString(styleFaint.Render("  (no sessions in this slice \u2014 move the cursor)") + "\n")
	} else {
		b.WriteString(styleHeading.Render(fmt.Sprintf("  %d session(s) here:", len(cur))) + "\n")
		for _, s := range cur {
			label := m.labelByID[s.ID()]
			name := s.ID()
			if label != "" {
				name = fmt.Sprintf("%s (%s)", label, s.ID())
			}
			top, _ := s.TopFolder()
			b.WriteString(fmt.Sprintf("    %s  %d files  %s\n",
				name, s.Count(), styleFaint.Render(top)))
		}
		b.WriteString("\n")

		files := m.currentFiles()
		b.WriteString(styleHeading.Render("  files:") + "\n")
		maxRows := m.fileRows()
		start, end := windowAround(m.fileCursor, len(files), maxRows)
		for i := start; i < end; i++ {
			f := files[i]
			line := fmt.Sprintf("%s  %s", f.EffectiveTime().Format("2006-01-02 15:04"), f.Path)
			if i == m.fileCursor {
				b.WriteString("  " + styleSelected.Render("\u276f "+line) + "\n")
			} else {
				b.WriteString("    " + line + "\n")
			}
		}
		if end < len(files) {
			b.WriteString(styleFaint.Render(fmt.Sprintf("    \u2026 %d more", len(files)-end)) + "\n")
		}
	}

	b.WriteString("\n")
	if m.status != "" {
		b.WriteString("  " + styleStatusMsg.Render(m.status) + "\n")
	}
	b.WriteString("  " + styleFaint.Render(
		"\u2190/\u2192 move  tab next  \u2191/\u2193 file  enter copy  o open  q quit") + "\n")

	return b.String()
}

// fileRows is how many file lines to show in the side pane, derived from the
// terminal height with a sane fallback.
func (m timelineModel) fileRows() int {
	if m.height <= 0 {
		return 10
	}
	// Reserve rows for the header/axis/session block and footer.
	r := m.height - 14
	if r < 3 {
		r = 3
	}
	return r
}

// windowAround returns a [start,end) slice window of size at most maxRows that
// keeps the cursor visible, scrolling as the cursor nears an edge.
func windowAround(cursor, n, maxRows int) (int, int) {
	if n <= maxRows {
		return 0, n
	}
	start := cursor - maxRows/2
	if start < 0 {
		start = 0
	}
	end := start + maxRows
	if end > n {
		end = n
		start = end - maxRows
	}
	return start, end
}

// copyToClipboard writes text to the OS clipboard using the platform's standard
// utility. It returns a clear error when no clipboard tool is available so the
// TUI can fall back to showing the path for manual copy.
func copyToClipboard(text string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "pbcopy"
	case "windows":
		name = "clip"
	default:
		// Prefer wl-copy (Wayland) then xclip then xsel.
		for _, cand := range []struct {
			bin  string
			args []string
		}{
			{"wl-copy", nil},
			{"xclip", []string{"-selection", "clipboard"}},
			{"xsel", []string{"--clipboard", "--input"}},
		} {
			if _, err := exec.LookPath(cand.bin); err == nil {
				name, args = cand.bin, cand.args
				break
			}
		}
		if name == "" {
			return fmt.Errorf("no clipboard tool found (install wl-copy, xclip, or xsel)")
		}
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// openFolder reveals a directory in the OS file browser. It is best-effort and
// never blocks the TUI.
func openFolder(dir string) error {
	if dir == "" {
		return fmt.Errorf("no folder for this file")
	}
	dir = filepath.Clean(dir)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("folder not accessible: %w", err)
	}
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name = "explorer"
	default:
		name = "xdg-open"
	}
	args = append(args, dir)
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("no opener (%s) found", name)
	}
	return exec.Command(name, args...).Start()
}

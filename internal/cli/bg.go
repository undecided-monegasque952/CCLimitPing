package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wavever/CCLimitPing/internal/config"
)

// bgUsageTimeout bounds each per-provider usage read in `bg status`.
const bgUsageTimeout = 30 * time.Second

// The background command runs `watch` as a detached process so a 5h window chain
// keeps going after the terminal closes. State lives next to the config:
//
//	<config dir>/bg.json   the running watcher's pid + metadata
//	<config dir>/bg.log    the watcher's combined stdout/stderr
//
// One background watcher runs at a time; `start` refuses to launch a second.
// Triggering still works detached because each provider allocates its own PTY;
// neither needs the parent's terminal.

const (
	bgStateName = "bg.json"
	bgLogName   = "bg.log"
)

// bgState is the persisted record of the background watcher.
type bgState struct {
	PID       int       `json:"pid"`
	Provider  string    `json:"provider"` // all|claude|codex
	DryRun    bool      `json:"dry_run"`
	StartedAt time.Time `json:"started_at"`
	LogPath   string    `json:"log_path"`
}

func newBackgroundCmd() *cobra.Command {
	text := localizedText()
	cmd := &cobra.Command{
		Use:     "background",
		Aliases: []string{"bg"},
		Short:   text.bgShort,
		Long:    text.bgLong,
		Example: text.bgExample,
		Args:    cobra.NoArgs,
		// Bare `limitping bg` reports status — the common "is it running?" check.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBgStatus(cmd.Context(), cmd.OutOrStdout())
		},
	}
	cmd.AddCommand(newBgStartCmd(), newBgStatusCmd(), newBgStopCmd(), newBgLogsCmd())
	return cmd
}

func newBgStartCmd() *cobra.Command {
	var dryRun bool
	text := localizedText()
	cmd := &cobra.Command{
		Use:       "start [provider]",
		Short:     text.bgStartShort,
		Long:      text.bgStartLong,
		Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"claude", "codex", "all"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBgStart(cmd.OutOrStdout(), argOrAll(args), dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, text.watchDryRunFlag)
	return cmd
}

func newBgStatusCmd() *cobra.Command {
	text := localizedText()
	return &cobra.Command{
		Use:   "status",
		Short: text.bgStatusShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBgStatus(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func newBgStopCmd() *cobra.Command {
	text := localizedText()
	return &cobra.Command{
		Use:   "stop",
		Short: text.bgStopShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBgStop(cmd.OutOrStdout())
		},
	}
}

func newBgLogsCmd() *cobra.Command {
	var follow bool
	var lines int
	text := localizedText()
	cmd := &cobra.Command{
		Use:   "logs",
		Short: text.bgLogsShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBgLogs(cmd.OutOrStdout(), lines, follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, text.bgLogsFollowFlag)
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, text.bgLogsLinesFlag)
	return cmd
}

// runBgStart launches `limitping watch` detached from the terminal.
func runBgStart(out io.Writer, provider string, dryRun bool) error {
	// Validate the provider selection up front so a misconfiguration fails here,
	// visibly, rather than silently in the detached log.
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if _, err := selectTargets(cfg, provider); err != nil {
		return err
	}

	if st, ok := readBgState(); ok && processAlive(st.PID) {
		return fmt.Errorf("background watch already running (pid %d); stop it first with `limitping bg stop`", st.PID)
	}

	dir, err := config.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(dir, bgLogName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening log %s: %w", logPath, err)
	}
	defer logFile.Close()

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating limitping binary: %w", err)
	}
	childArgs := []string{"watch"}
	if provider != "" && provider != "all" {
		childArgs = append(childArgs, provider)
	}
	if dryRun {
		childArgs = append(childArgs, "--dry-run")
	}

	child := exec.Command(exe, childArgs...)
	child.Stdin = nil // /dev/null
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = detachSysProcAttr()
	if err := child.Start(); err != nil {
		return fmt.Errorf("starting background watch: %w", err)
	}
	pid := child.Process.Pid
	_ = child.Process.Release() // detach from the child; this also zeroes Pid, so read it first

	st := bgState{
		PID:       pid,
		Provider:  provider,
		DryRun:    dryRun,
		StartedAt: time.Now(),
		LogPath:   logPath,
	}
	if err := writeBgState(st); err != nil {
		return fmt.Errorf("recording background state: %w", err)
	}

	text := localizedText()
	fmt.Fprintf(out, text.bgStartedFmt, st.PID, provider, dryRunNote(dryRun))
	fmt.Fprintf(out, text.bgLogPathFmt, logPath)
	fmt.Fprintln(out, text.bgStartFollowUp)
	return nil
}

// runBgStatus reports whether the background watcher is running. When it is, it
// resolves the watched provider selection (so "all" expands to the real list)
// and prints each provider's current usage, ending with a hint at the relevant
// subcommands so they stay discoverable from bare `bg`.
func runBgStatus(ctx context.Context, out io.Writer) error {
	text := localizedText()
	st, ok := readBgState()
	if !ok {
		fmt.Fprintln(out, text.bgNotRunning)
		fmt.Fprintln(out, text.bgHintStart)
		return nil
	}
	if !processAlive(st.PID) {
		_ = removeBgState() // stale: the process died without cleaning up
		fmt.Fprintf(out, text.bgClearedStaleFmt, st.PID)
		fmt.Fprintln(out, text.bgHintStart)
		return nil
	}
	fmt.Fprintf(out, text.bgRunningFmt, st.PID)
	fmt.Fprintf(out, "  %s: %s\n", text.bgFieldUptime, time.Since(st.StartedAt).Round(time.Second))
	fmt.Fprintf(out, "  %s: %s\n", text.bgFieldStarted, st.StartedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(out, "  %s: %s\n", text.bgFieldLogs, st.LogPath)

	// Resolve the watched selection to the actual providers. On failure (e.g.
	// nothing enabled in config now), fall back to the raw selection rather than
	// failing the whole status read.
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	providers, perr := selectProviders(cfg, st.Provider)
	if perr != nil {
		fmt.Fprintf(out, "  %s: %s%s\n", text.bgFieldWatching, st.Provider, dryRunNote(st.DryRun))
		fmt.Fprintln(out, text.bgHintManage)
		return nil
	}

	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	fmt.Fprintf(out, "  %s: %s%s\n", text.bgFieldWatching, strings.Join(names, ", "), dryRunNote(st.DryRun))

	// Per-provider usage, the same view as `limitping status`.
	fmt.Fprintln(out)
	for _, p := range providers {
		rctx, cancel := context.WithTimeout(ctx, bgUsageTimeout)
		u, uerr := p.ReadUsage(rctx)
		cancel()
		if uerr != nil {
			fmt.Fprintf(out, "%-7s  error: %v\n\n", p.Name(), uerr)
			continue
		}
		printUsage(out, u, false)
	}

	fmt.Fprintln(out, text.bgHintManage)
	return nil
}

// runBgStop terminates the background watcher gracefully.
func runBgStop(out io.Writer) error {
	text := localizedText()
	st, ok := readBgState()
	if !ok {
		fmt.Fprintln(out, text.bgNotRunning)
		return nil
	}
	if !processAlive(st.PID) {
		_ = removeBgState()
		fmt.Fprintf(out, text.bgStopWasStaleFmt, st.PID)
		return nil
	}
	if err := terminateProcess(st.PID); err != nil {
		return fmt.Errorf("stopping pid %d: %w", st.PID, err)
	}
	// watch handles SIGTERM via signal.NotifyContext; give it a moment to exit.
	for i := 0; i < 50 && processAlive(st.PID); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	_ = removeBgState()
	fmt.Fprintf(out, text.bgStoppedFmt, st.PID)
	return nil
}

// runBgLogs prints the tail of the background watcher's log, optionally following.
func runBgLogs(out io.Writer, lines int, follow bool) error {
	logPath, err := bgLogPath()
	if err != nil {
		return err
	}
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(out, localizedText().bgNoLogYetFmt, logPath)
			return nil
		}
		return err
	}
	defer f.Close()

	if err := printLastLines(out, f, lines); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	return followFile(out, f)
}

// printLastLines writes the final n lines of f and leaves f positioned at EOF so
// a follower can continue from there. n <= 0 prints nothing.
func printLastLines(out io.Writer, f *os.File, n int) error {
	if n <= 0 {
		_, err := f.Seek(0, io.SeekEnd)
		return err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	ring := make([]string, 0, n)
	for sc.Scan() {
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return err
	}
	for _, line := range ring {
		fmt.Fprintln(out, line)
	}
	return nil
}

// followFile streams content appended to f (already positioned at EOF) until the
// process is interrupted, like `tail -f`.
func followFile(out io.Writer, f *os.File) error {
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Fprint(out, line)
		}
		switch err {
		case nil:
			continue
		case io.EOF:
			time.Sleep(500 * time.Millisecond)
		default:
			return err
		}
	}
}

// dryRunNote returns a human-readable suffix for the dry-run flag.
func dryRunNote(dryRun bool) string {
	if dryRun {
		return " (dry-run)"
	}
	return ""
}

func bgStatePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, bgStateName), nil
}

func bgLogPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, bgLogName), nil
}

// readBgState loads the persisted state; ok is false when it's missing or invalid.
func readBgState() (bgState, bool) {
	path, err := bgStatePath()
	if err != nil {
		return bgState{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return bgState{}, false
	}
	var st bgState
	if err := json.Unmarshal(data, &st); err != nil || st.PID <= 0 {
		return bgState{}, false
	}
	return st, true
}

func writeBgState(st bgState) error {
	path, err := bgStatePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func removeBgState() error {
	path, err := bgStatePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

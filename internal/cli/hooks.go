package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wavever/CCLimitPing/internal/activity"
)

// Hooks let limitping detect whether a Claude/Codex session is actually mid-turn
// (instead of merely having a live process). install/uninstall register or strip
// limitping's hook entries in each CLI's config; the entries invoke the hidden
// `limitping hook <provider>` command (see hook.go).

func newHooksCmd() *cobra.Command {
	text := localizedText()
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: text.hooksShort,
		Long:  text.hooksLong,
	}
	cmd.AddCommand(newHooksInstallCmd(), newHooksUninstallCmd())
	return cmd
}

func newHooksInstallCmd() *cobra.Command {
	text := localizedText()
	return &cobra.Command{
		Use:       "install [provider]",
		Short:     text.hooksInstallShort,
		Long:      text.hooksInstallLong,
		Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"claude", "codex", "all"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooks(cmd.OutOrStdout(), argOrAll(args), true)
		},
	}
}

func newHooksUninstallCmd() *cobra.Command {
	text := localizedText()
	return &cobra.Command{
		Use:       "uninstall [provider]",
		Aliases:   []string{"rm", "remove"},
		Short:     text.hooksUninstallShort,
		Long:      text.hooksUninstallLong,
		Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"claude", "codex", "all"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooks(cmd.OutOrStdout(), argOrAll(args), false)
		},
	}
}

func runHooks(out io.Writer, name string, install bool) error {
	text := localizedText()
	providers := resolveHookProviders(name)

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating limitping binary: %w", err)
	}

	for _, p := range providers {
		path, spec, err := providerHookSpec(p, binPath)
		if err != nil {
			return err
		}
		changed, err := applyHooks(path, spec, install)
		if err != nil {
			return fmt.Errorf("%s hooks: %w", p, err)
		}
		if err := activity.SetEnabled(p, install); err != nil {
			return fmt.Errorf("%s hooks: %w", p, err)
		}
		switch {
		case install:
			fmt.Fprintf(out, text.hooksInstalledFmt, p, path)
		case changed:
			fmt.Fprintf(out, text.hooksRemovedFmt, p, path)
		default:
			fmt.Fprintf(out, text.hooksNothingFmt, p, path)
		}
	}

	if install {
		fmt.Fprint(out, text.hooksTrustNote)
	}
	return nil
}

// removeHooksBestEffort strips limitping's hook entries from every provider's CLI
// config. Used during uninstall so we don't leave hooks pointing at a deleted
// binary; failures are reported but never abort the uninstall. Removal matches by
// predicate, so it works even when the binary path can't be resolved.
func removeHooksBestEffort(errOut io.Writer) {
	binPath, _ := os.Executable()
	for _, p := range []string{"claude", "codex"} {
		path, spec, err := providerHookSpec(p, binPath)
		if err != nil {
			continue
		}
		if _, err := applyHooks(path, spec, false); err != nil {
			fmt.Fprintf(errOut, "warning: removing %s hooks: %v\n", p, err)
			continue
		}
		_ = activity.SetEnabled(p, false)
	}
}

// resolveHookProviders maps the CLI arg to the providers that support hooks.
func resolveHookProviders(name string) []string {
	switch name {
	case "", "all":
		return []string{"claude", "codex"}
	default:
		return []string{name}
	}
}

func argOrAll(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "all"
}

func providerHookSpec(provider, binPath string) (string, hookSpec, error) {
	switch provider {
	case "claude":
		path, err := claudeSettingsPath()
		return path, claudeSpec(binPath), err
	case "codex":
		path, err := codexHooksPath()
		return path, codexSpec(binPath), err
	}
	return "", hookSpec{}, fmt.Errorf("provider %q has no CLI hooks", provider)
}

// hookSpec describes a provider's hook wiring: which events to register, the
// handler to insert, and how to recognize our own previously-installed handler.
type hookSpec struct {
	events  []string
	handler map[string]any
	isOurs  func(handler map[string]any) bool
}

// claudeSpec wires ~/.claude/settings.json. Claude command hooks take a command
// plus an args array, so path quoting is a non-issue.
func claudeSpec(binPath string) hookSpec {
	return hookSpec{
		events: []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "SessionEnd"},
		handler: map[string]any{
			"type":    "command",
			"command": binPath,
			"args":    []any{"hook", "claude"},
		},
		isOurs: func(h map[string]any) bool {
			return argsEqual(h["args"], "hook", "claude")
		},
	}
}

// codexSpec wires ~/.codex/hooks.json. Codex command hooks take a single command
// string, so the binary path is quoted if it contains spaces. Codex has no
// SessionEnd event — the activity TTL covers abandoned sessions.
func codexSpec(binPath string) hookSpec {
	return hookSpec{
		events: []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop"},
		handler: map[string]any{
			"type":    "command",
			"command": quoteCmd(binPath) + " hook codex",
		},
		isOurs: func(h map[string]any) bool {
			s, _ := h["command"].(string)
			return strings.Contains(s, "hook codex")
		},
	}
}

// applyHooks merges (install) or strips (uninstall) our hook entries in a JSON
// hooks file, preserving any other settings and making a .bak first. It reports
// whether the file was written.
func applyHooks(path string, spec hookSpec, install bool) (bool, error) {
	if !install && !fileExists(path) {
		return false, nil // nothing to remove
	}
	root, err := loadJSONObject(path)
	if err != nil {
		return false, err
	}
	mergeHooks(root, spec, install)
	if err := writeJSONWithBackup(path, root); err != nil {
		return false, err
	}
	return true, nil
}

// mergeHooks ensures each of spec.events holds exactly one of our matcher-groups
// (any prior copy is removed first, for idempotency). When install is false it
// only removes ours and drops the now-empty event arrays.
func mergeHooks(root map[string]any, spec hookSpec, install bool) {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, event := range spec.events {
		arr, _ := hooks[event].([]any)
		arr = removeOurGroups(arr, spec.isOurs)
		if install {
			arr = append(arr, ourGroup(spec.handler))
		}
		if len(arr) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = arr
		}
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = hooks
	}
}

func ourGroup(handler map[string]any) map[string]any {
	return map[string]any{
		"matcher": "*",
		"hooks":   []any{handler},
	}
}

func removeOurGroups(arr []any, isOurs func(map[string]any) bool) []any {
	out := arr[:0:0] // fresh backing array; never aliases the input
	for _, g := range arr {
		group, ok := g.(map[string]any)
		if ok && groupContainsOurs(group, isOurs) {
			continue
		}
		out = append(out, g)
	}
	return out
}

func groupContainsOurs(group map[string]any, isOurs func(map[string]any) bool) bool {
	handlers, ok := group["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range handlers {
		if handler, ok := h.(map[string]any); ok && isOurs(handler) {
			return true
		}
	}
	return false
}

func argsEqual(v any, want ...string) bool {
	arr, ok := v.([]any)
	if !ok || len(arr) != len(want) {
		return false
	}
	for i, w := range want {
		if s, ok := arr[i].(string); !ok || s != w {
			return false
		}
	}
	return true
}

func quoteCmd(path string) string {
	if strings.ContainsAny(path, " \t") {
		return `"` + path + `"`
	}
	return path
}

func claudeSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func codexHooksPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "hooks.json"), nil
}

func loadJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func writeJSONWithBackup(path string, obj any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := backupFile(path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func backupFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to back up
		}
		return err
	}
	return os.WriteFile(path+".bak", data, 0o644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

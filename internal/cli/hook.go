package cli

import (
	"encoding/json"
	"io"

	"github.com/spf13/cobra"

	"github.com/wavever/CCLimitPing/internal/activity"
)

// maxHookInput caps how much of stdin we read; hook payloads are tiny.
const maxHookInput = 1 << 20

// hookEvent is the subset of the hook stdin JSON we need. Claude Code and Codex
// pipe the same shape: {"session_id": ..., "hook_event_name": ...}.
type hookEvent struct {
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
}

// newHookCmd is the hidden command the Claude/Codex hook frameworks invoke (see
// `limitping hooks install`). It records turn activity and intentionally never
// fails loudly or writes to stdout, so it can't perturb the host CLI's hook
// pipeline. Installed via `limitping hooks install`.
func newHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "hook [provider]",
		Hidden:        true,
		Args:          cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs:     []string{"claude", "codex"},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			recordHookEvent(cmd.InOrStdin(), args[0])
			return nil
		},
	}
}

// recordHookEvent decodes a hook payload and updates the activity store. All
// errors are swallowed: a hook must be a no-op on bad input rather than fail.
func recordHookEvent(stdin io.Reader, provider string) {
	data, err := io.ReadAll(io.LimitReader(stdin, maxHookInput))
	if err != nil || len(data) == 0 {
		return
	}
	var ev hookEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}
	switch {
	case activity.IsStopEvent(ev.HookEventName):
		_ = activity.Clear(provider, ev.SessionID)
	case activity.IsRunningEvent(ev.HookEventName):
		_ = activity.Mark(provider, ev.SessionID, ev.HookEventName)
	}
}

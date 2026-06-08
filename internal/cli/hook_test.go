package cli

import (
	"strings"
	"testing"

	"github.com/wavever/CCLimitPing/internal/activity"
)

func TestRecordHookEventRunningThenStop(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	recordHookEvent(strings.NewReader(`{"session_id":"s1","hook_event_name":"UserPromptSubmit"}`), "claude")
	if _, active, _ := activity.Active("claude"); !active {
		t.Fatal("UserPromptSubmit should mark the session active")
	}

	recordHookEvent(strings.NewReader(`{"session_id":"s1","hook_event_name":"Stop"}`), "claude")
	if _, active, _ := activity.Active("claude"); active {
		t.Fatal("Stop should clear the session")
	}
}

func TestRecordHookEventToleratesBadInput(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// None of these should panic or mark anything active.
	recordHookEvent(strings.NewReader(""), "claude")
	recordHookEvent(strings.NewReader("not json"), "claude")
	recordHookEvent(strings.NewReader(`{"hook_event_name":"Notification"}`), "claude")
	if _, active, _ := activity.Active("claude"); active {
		t.Fatal("bad/ignored input must not mark active")
	}
}

package wecom

import (
	"fmt"
	"time"
)

// StatusLine renders the single read-only line the TUI shows. It is
// deliberately narrow: the terminal view reports state and the last activity
// time, and nothing else. Injecting, reconfiguring, and confirming writes all
// happen in the web console where they can be authenticated.
//
// Examples:
//
//	wecom: off
//	wecom: mock   idle
//	wecom: mock   in=1 out=2  last=12:03:44
//	wecom: connected  in=3 out=3  last=12:07:11
func StatusLine(s Snapshot) string {
	mode := s.Mode
	if mode == "" {
		mode = string(ModeOff)
	}
	if !s.Enabled || mode == string(ModeOff) {
		return "wecom: off"
	}

	state := s.State
	if state == "" {
		state = string(StateDisconnected)
	}

	line := fmt.Sprintf("wecom: %s", state)

	total := s.Counts.Inbound + s.Counts.Outbound
	if total == 0 {
		return line + "  idle"
	}

	line += fmt.Sprintf("  in=%d out=%d", s.Counts.Inbound, s.Counts.Outbound)
	if !s.LastActivity.IsZero() {
		line += "  last=" + s.LastActivity.Local().Format(time.TimeOnly)
	}
	return line
}

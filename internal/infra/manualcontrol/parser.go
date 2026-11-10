package manualcontrol

import (
	"regexp"
	"strings"
	"time"
)

// Operation records intent, not the capture-state notifications shared by
// manual commands and NVIDIA's automatic shutdowns.
type Operation struct {
	Time    string `json:"time"`
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"`
}

type parser struct {
	HotkeyTime string `json:"hotkey_time,omitempty"`
	HotkeyBody bool   `json:"hotkey_body,omitempty"`
	DVRToggle  bool   `json:"dvr_toggle,omitempty"`
}

var logHeader = regexp.MustCompile(`^(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d\.\d{3}) (INFO|WARN|ERROR|DEBUG)\s+(\S+)\s+(.*)$`)

const logTimeLayout = "2006-01-02 15:04:05.000"

func (p *parser) line(line string) *Operation {
	parts := logHeader.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
	if parts == nil {
		if p.HotkeyBody && strings.TrimSpace(line) == `"hotkey": "DVRToggle",` {
			p.DVRToggle = true
		}
		return nil
	}
	at, level, service, message := parts[1], parts[2], parts[3], strings.TrimSpace(parts[4])
	if level != "INFO" {
		*p = parser{}
		return nil
	}
	p.HotkeyBody = false
	if service == "IntantReplay" && (message == "on toggle IR:  false" || message == "on toggle IR:  true") {
		*p = parser{}
		return &Operation{Time: at, Enabled: strings.HasSuffix(message, "true"), Source: "overlay"}
	}
	if service == "HotkeyService" && message == "Hotkey:  {" {
		*p = parser{HotkeyTime: at, HotkeyBody: true}
		return nil
	}
	if p.HotkeyTime == "" {
		return nil
	}
	start, err := time.Parse(logTimeLayout, p.HotkeyTime)
	end, parseErr := time.Parse(logTimeLayout, at)
	if err != nil || parseErr != nil || end.Before(start) || end.Sub(start) > time.Second || service == "McpOverlayService" {
		*p = parser{}
		return nil
	}
	if service == "ShadowPlayService" {
		// The command is logged immediately after the shortcut. Clear the
		// candidate on every other ShadowPlay entry, especially Capture Event,
		// so an automatic stop cannot become a user operation.
		matched := p.DVRToggle && (message == "IR Disabled" || message == "IR Enabled")
		*p = parser{}
		if matched {
			return &Operation{Time: at, Enabled: message == "IR Enabled", Source: "hotkey"}
		}
	}
	// Only the immediately following log entry may be the shortcut command.
	// Do not correlate later state changes with a blocked or ignored shortcut.
	*p = parser{}
	return nil
}

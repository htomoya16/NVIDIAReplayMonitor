package manualcontrol

import (
	"strings"
	"testing"
)

func TestParserRecognizesIntentOnly(t *testing.T) {
	for _, test := range []struct {
		name, text string
		want       int
	}{
		{"UI", ui("19:24:03.772", "false") + ui("19:24:09.933", "true"), 2},
		{"shortcuts", hotkey("19:34:34.675", "IR Disabled") + hotkey("19:34:40.127", "IR Enabled"), 2},
		{"automatic stop", "2026-11-10 17:07:38.007 INFO  ShadowPlayService  Capture Event: {\n\"recordingState\": \"disabled\",\n}\n2026-11-10 17:07:38.009 INFO  ShadowPlayService  IR Disabled \n", 0},
		{"other hotkey", strings.ReplaceAll(hotkey("19:34:34.675", "IR Disabled"), "DVRToggle", "OpenIGO"), 0},
		{"expired hotkey", strings.Replace(hotkey("19:34:34.675", "IR Disabled"), "19:34:34.675 INFO  ShadowPlayService", "19:34:36.675 INFO  ShadowPlayService", 1), 0},
		{"blocked hotkey then state event", strings.Replace(hotkey("19:34:34.675", "IR Disabled"), "INFO  ShadowPlayService  IR Disabled", "INFO  ShadowPlayService  Capture Event: {}\n2026-11-10 19:34:34.676 INFO  ShadowPlayService  IR Disabled", 1), 0},
		{"intervening event", strings.Replace(hotkey("19:34:34.675", "IR Disabled"), "INFO  ShadowPlayService  IR Disabled", "INFO  OtherService  ignored\n2026-11-10 19:34:34.676 INFO  ShadowPlayService  IR Disabled", 1), 0},
		{"MCP command", "2026-11-10 19:34:34.675 INFO  McpOverlayService  MCP overlay tool call received\n2026-11-10 19:34:34.676 INFO  ShadowPlayService  IR Disabled\n", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var p parser
			var operations []*Operation
			for _, line := range strings.Split(test.text, "\n") {
				if op := p.line(line); op != nil {
					operations = append(operations, op)
				}
			}
			if len(operations) != test.want {
				t.Fatalf("operations=%+v", operations)
			}
		})
	}
}

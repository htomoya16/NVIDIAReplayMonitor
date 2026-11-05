// Package notices embeds redistribution notices in the standalone executable.
package notices

import _ "embed"

//go:embed third_party.txt
var Text string

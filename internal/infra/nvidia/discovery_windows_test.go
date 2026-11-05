package nvidia

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFiles(t *testing.T) {
	for _, test := range []struct {
		name, content, want string
		wantErr             bool
	}{
		{"fallback port", `{"http":"http://127.0.0.1:13509/mcp/"}`, "http://127.0.0.1:13509/mcp/", false},
		{"pipe only", `{"pipe":"test"}`, "", true},
		{"malformed", `{`, "", true},
		{"external address", `{"http":"http://example.com:13508/mcp"}`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "server.json")
			if err := os.WriteFile(path, []byte(test.content), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := discoverFiles([]string{path + "-missing", path})
			if got != test.want || (err != nil) != test.wantErr {
				t.Fatalf("endpoint=%q err=%v", got, err)
			}
		})
	}
	got, err := discoverFiles([]string{filepath.Join(t.TempDir(), "absent.json")})
	if err != nil || got != DefaultEndpoint {
		t.Fatalf("fallback=%q err=%v", got, err)
	}
}

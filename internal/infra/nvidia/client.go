package nvidia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	DefaultEndpoint = "http://127.0.0.1:13508/mcp"
	statusTool      = "nvapp_overlay_get_status"
	captureTool     = "nvapp_overlay_capture"
)

// Client is used serially by one monitor (or one doctor invocation).
// Session recovery happens only on the next caller request, never in a retry loop.
type Client struct {
	resolve   func() (string, error)
	version   string
	http      *http.Client
	session   *mcp.ClientSession
	endpoint  string
	canEnable bool
}

func NewClient(version string, resolve func() (string, error)) *Client {
	return &Client{
		resolve: resolve, version: version,
		http: &http.Client{
			Timeout:       8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport: &http.Transport{
				Proxy:        nil,
				DialContext:  (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: -1}).DialContext,
				MaxIdleConns: 1, MaxIdleConnsPerHost: 1, IdleConnTimeout: 90 * time.Second,
				ResponseHeaderTimeout: 5 * time.Second,
			},
		},
	}
}

func (c *Client) Endpoint() string      { return c.endpoint }
func (c *Client) CanEnableReplay() bool { return c.canEnable }

func (c *Client) connect(ctx context.Context) error {
	if c.session != nil {
		return nil
	}
	endpoint, err := c.resolve()
	if err != nil {
		return err
	}
	if err := ValidateEndpoint(endpoint); err != nil {
		return err
	}
	c.endpoint = endpoint
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := mcp.NewClient(&mcp.Implementation{Name: "NVIDIAReplayMonitor", Version: c.version}, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{}, Logger: logger,
	})
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: endpoint, HTTPClient: c.http, DisableStandaloneSSE: true, MaxRetries: -1,
	}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		return fmt.Errorf("MCP接続に失敗: %w", err)
	}
	c.session = session
	if err := c.loadToolCapabilities(ctx); err != nil {
		c.closeSession()
		return err
	}
	return nil
}

func (c *Client) loadToolCapabilities(ctx context.Context) error {
	var tools []*mcp.Tool
	for cursor := ""; ; {
		listed, err := c.session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return fmt.Errorf("ツール一覧を取得できません: %w", err)
		}
		tools = append(tools, listed.Tools...)
		if listed.NextCursor == "" {
			break
		}
		if listed.NextCursor == cursor || len(tools) > 1000 {
			return errors.New("ツール一覧のページ情報が不正です")
		}
		cursor = listed.NextCursor
	}
	var hasStatus bool
	for _, tool := range tools {
		if tool.Name == statusTool {
			hasStatus = true
		}
		if tool.Name == captureTool {
			c.canEnable = supportsEnable(tool.InputSchema)
		}
	}
	if !hasStatus {
		return errors.New("状態取得ツールがありません。NVIDIA AppのMCPアクセス設定を確認してください")
	}
	return nil
}

func supportsEnable(schema any) bool {
	var input struct {
		Type       string `json:"type"`
		Properties map[string]struct {
			Type string   `json:"type"`
			Enum []string `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	b, err := json.Marshal(schema)
	if err != nil || json.Unmarshal(b, &input) != nil {
		return false
	}
	action, enable := input.Properties["action"], input.Properties["enable"]
	if input.Type != "object" || action.Type != "string" || enable.Type != "boolean" || !slices.Contains(action.Enum, "toggle_instant_replay") {
		return false
	}
	for _, required := range input.Required {
		if required != "action" && required != "enable" {
			return false
		}
	}
	return true
}

func (c *Client) call(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		c.closeSession()
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if result == nil {
		return nil, errors.New("MCPから結果が返りませんでした")
	}
	if result.IsError {
		return nil, fmt.Errorf("%s: %s", name, resultMessage(result))
	}
	return result, nil
}

func (c *Client) IsReplayEnabled(ctx context.Context) (bool, error) {
	result, err := c.call(ctx, statusTool, map[string]any{})
	if err != nil {
		return false, err
	}
	return parseReplayEnabled(result.StructuredContent)
}

// CanRestartOverlay requires explicit idle states. Missing fields are unknown,
// and neither a recording nor an Instant Replay buffer is interrupted.
func (c *Client) CanRestartOverlay(ctx context.Context) error {
	result, err := c.call(ctx, statusTool, map[string]any{})
	if err != nil {
		return err
	}
	return parseOverlayIdle(result.StructuredContent)
}

func parseOverlayIdle(content any) error {
	var state struct {
		Recording *bool `json:"recordingActive"`
		Replay    *bool `json:"instantReplayEnabled"`
	}
	b, err := json.Marshal(content)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &state); err != nil {
		return err
	}
	if state.Recording == nil || state.Replay == nil {
		return errors.New("録画状態が不明なためOverlayを終了しません。Overlayを手動でオフにして再実行してください")
	}
	if *state.Recording || *state.Replay {
		return errors.New("録画またはインスタントリプレイが動作中です。オフにしてから再実行してください")
	}
	return nil
}

func parseReplayEnabled(content any) (bool, error) {
	var state struct {
		Enabled *bool `json:"instantReplayEnabled"`
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return false, fmt.Errorf("状態を解釈できません: %w", err)
	}
	if err := json.Unmarshal(encoded, &state); err != nil {
		return false, fmt.Errorf("状態の型が不正です: %w", err)
	}
	if state.Enabled == nil {
		return false, errors.New("instantReplayEnabledが返されていないため、状態は不明です")
	}
	return *state.Enabled, nil
}

func (c *Client) EnableReplay(ctx context.Context) error {
	if err := c.connect(ctx); err != nil {
		return err
	}
	if !c.canEnable {
		// Re-discover capabilities next cycle in case the user changes access.
		c.closeSession()
		return errors.New("有効化ツールが利用できません。MCPのstandard以上のアクセス設定を確認してください")
	}
	_, err := c.call(ctx, captureTool, map[string]any{"action": "toggle_instant_replay", "enable": true})
	// A returned message alone is not treated as independently confirmed state.
	return err
}

func resultMessage(result *mcp.CallToolResult) string {
	var messages []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			messages = append(messages, text.Text)
		}
	}
	if len(messages) == 0 {
		return "NVIDIA Appが操作エラーを返しました"
	}
	return strings.Join(messages, "\n")
}

func (c *Client) closeSession() {
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
	}
	c.canEnable = false
}

func (c *Client) Close() {
	c.closeSession()
	c.http.CloseIdleConnections()
}

func ValidateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return errors.New("MCP接続先URLが不正です")
	}
	if u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "/mcp" && u.Path != "/mcp/") || u.Port() == "" {
		return errors.New("MCP接続先はポート付きのローカルHTTP /mcpに限定されています")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return errors.New("MCP接続先はループバックIPに限定されています")
	}
	return nil
}

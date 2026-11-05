package nvidia

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func DiscoverEndpoint() (string, error) {
	local, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", fmt.Errorf("ユーザーフォルダを取得できません: %w", err)
	}
	// The second location is used by the installed 11.0.9.585 build.
	paths := []string{
		filepath.Join(local, "NVIDIA Corporation", "NvAppMcpServer", "server.json"),
		filepath.Join(local, "NVIDIA Corporation", "NVIDIA App", "McpServer", "server.json"),
	}
	return discoverFiles(paths)
}

func discoverFiles(paths []string) (string, error) {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("NVIDIA接続情報を読み取れません: %w", err)
		}
		if len(data) > 64*1024 {
			return "", errors.New("NVIDIA接続情報のサイズが不正です")
		}
		var server struct {
			HTTP string `json:"http"`
		}
		if err := json.Unmarshal(data, &server); err != nil {
			return "", fmt.Errorf("NVIDIA接続情報が不正です: %w", err)
		}
		if server.HTTP == "" {
			return "", errors.New("NVIDIA MCPのHTTP接続先がありません。NVIDIA AppのMCP設定を確認してください")
		}
		if err := ValidateEndpoint(server.HTTP); err != nil {
			return "", err
		}
		return server.HTTP, nil
	}
	return DefaultEndpoint, nil
}

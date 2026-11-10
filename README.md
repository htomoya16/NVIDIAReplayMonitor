# NVIDIAReplayMonitor

NVIDIA Appのインスタントリプレイを30秒間隔で確認し、オフの場合に有効化するWindows常駐ツールです。

## 動作条件

| 項目 | 条件 |
| --- | --- |
| OS | Windows 11（x64） |
| GPU | NVIDIA Appの録画機能に対応したNVIDIA GPU |
| NVIDIA App | 11.0.9.585で動作確認済み |
| オーバーレイ | 「NVIDIA オーバーレイ」をオン |
| エージェントとアプリの接続 | 「AI エージェントが NVIDIA App を制御できるようにする」をオン |

NVIDIA Appを開き、「設定」から「オーバーレイ」と「エージェントとアプリの接続」の各スイッチをオンにしてください。

## インストール

1. GitHubの[Releases](https://github.com/htomoya16/NVIDIAReplayMonitor/releases)を開く。
2. `NVIDIAReplayMonitor-<バージョン>-windows-amd64.zip` をダウンロードする。
3. ZIPを右クリック →「すべて展開」を選ぶ。
4. `NVIDIAReplayMonitor-<バージョン>-Setup.exe` を実行する。
5. インストール先を選ぶ。既定値は `%LOCALAPPDATA%\NVIDIAReplayMonitor`。
6. 「Windowsログイン時に起動する」「インストール完了後に起動する」を選び、インストールする。

自動起動を選択すると、以後はWindowsへのログイン時に起動します。ショートカットの作成や名前変更は不要です。

## 更新

1. [Releases](https://github.com/htomoya16/NVIDIAReplayMonitor/releases)から新しいZIPをダウンロードして展開する。
2. 新しい `NVIDIAReplayMonitor-<バージョン>-Setup.exe` を実行する。
3. 画面の案内に従って更新する。

監視は更新前に停止します。既存のインストール先、自動起動の選択、保留状態、NVIDIA設定の復元情報を引き継ぎます。事前のアンインストールは不要です。

## アンインストール

1. NVIDIA Appの録画・インスタントリプレイをオフにする。
2. Windowsの「設定」→「アプリ」→「インストールされているアプリ」から、NVIDIAReplayMonitorをアンインストールする。

インストール先の `unins000.exe` からもアンインストールできます。

監視停止、通知設定の復元、自動起動解除後、インストールしたファイルを削除します。設定の復元に失敗した場合は削除を中止するため、表示された内容を確認して再実行してください。

## 操作

配置したフォルダでPowerShellを開いて実行します。

| 操作 | コマンド |
| --- | --- |
| 起動 | `.\NVIDIAReplayMonitor.exe` |
| 停止 | `.\NVIDIAReplayMonitor.exe --stop` |
| 状態確認 | `.\NVIDIAReplayMonitor.exe --doctor` |

NVIDIAオーバーレイのメニュー・設定スイッチ、またはインスタントリプレイ切替ショートカットで手動オフにすると、自動復帰を保留します。手動オンにすると再開します。操作は次の30秒周期までに反映され、保留状態はPC再起動後も維持します。初回起動より前の操作は対象外です。

Discordで配信する間だけ停止したい場合は、配信前に手動オフにし、終了後に手動オンにしてください。配信開始を検知して自動で保留する機能はありません。DRMによる録画制限中は録画できません。

手動操作の判別にはNVIDIA Overlayの内部ログを使用します。ログが読めない場合は自動復帰を控え、次の周期に再確認します。NVIDIA Appの更新でログ形式が変わると、手動操作を判別できなくなる場合があります。

## 通知の抑止

DRM保護コンテンツの視聴中は、30秒ごとの復帰試行によってNVIDIAの警告通知が表示されます。この警告だけを抑止するため、初回の復帰試行前にNVIDIA Appの通知設定を変更しています。

変更は、NVIDIA Overlayの内部DBにある以下の設定です。

| 項目 | 内容 |
| --- | --- |
| データベース | `shareclient` |
| 設定領域 | `IgoNotifications` |
| 設定名 | `noteProtectedContentWarning` |
| 変更値 | `false` |

設定変更時はNVIDIA オーバーレイを再起動します。録画・インスタントリプレイが動作中の場合は、オフになってから設定します。変更した通知設定はアンインストール時に元へ戻します。

## 問題がある場合

1. `--doctor` で状態を確認する。
2. 動作条件の設定を確認する。

アンインストール時にOverlayの終了確認で止まる場合は、NVIDIA Appの「設定」から「NVIDIA オーバーレイ」をオフにして再試行してください。削除後はオンに戻してください。

| 記録 | 保存先 |
| --- | --- |
| 診断結果 | `%LOCALAPPDATA%\NVIDIAReplayMonitor\doctor.txt` |
| 手動操作・保留状態の保存情報 | `%LOCALAPPDATA%\NVIDIAReplayMonitor\manual-control.json` |

## 参考資料

- [NVIDIA App MCPの操作仕様](https://github.com/NVIDIA/nvidia-app-skills/blob/main/skills/nvidia-app/references/mcp-tool-contract.md)
- [NVIDIA App MCPの接続仕様](https://github.com/NVIDIA/nvidia-app-skills/blob/main/skills/nvidia-app/references/connection.md)

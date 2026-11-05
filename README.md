# NVIDIAReplayMonitor

NVIDIA Appのインスタントリプレイを30秒間隔で確認し、オフの場合に有効化するWindows常駐ツールです。

## 動作条件

| 項目 | 条件 |
| --- | --- |
| OS | Windows 11（64bit） |
| GPU | NVIDIA Appの録画機能に対応したNVIDIA GPU |
| NVIDIA App | 11.0.9.585で動作確認済み |
| NVIDIA Appの設定 | NVIDIAオーバーレイと「エージェントとアプリの接続」をオン |
| MCPアクセス | standard以上 |

## 導入・自動起動

1. GitHubの[Releases](https://github.com/htomoya16/NVIDIAReplayMonitor/releases)を開く。
2. `NVIDIAReplayMonitor.exe` と `uninstall.cmd` をダウンロードする。
3. `Win + R` → `%LOCALAPPDATA%` を開き、`NVIDIAReplayMonitor` フォルダを作成する。
4. ダウンロードした2ファイルを `%LOCALAPPDATA%\NVIDIAReplayMonitor` に置く。
5. `NVIDIAReplayMonitor.exe` を右クリック →「その他のオプションを確認」→「ショートカットの作成」を選ぶ。
6. 作成されたショートカットの名前を `NVIDIAReplayMonitor` に変更する。
7. `Win + R` → `shell:startup` で開いたフォルダへ、そのショートカットを移動する。
8. ショートカットをダブルクリックして起動する。以後はWindowsへのログイン時に起動する。

初回は、警告通知の設定のためNVIDIAオーバーレイを再起動します。録画・インスタントリプレイが動作中の場合は、オフになってから設定します。

## 操作

配置したフォルダでPowerShellを開いて実行します。

| 操作 | コマンド |
| --- | --- |
| 起動 | `.\NVIDIAReplayMonitor.exe` |
| 停止 | `.\NVIDIAReplayMonitor.exe --stop` |
| 状態確認 | `.\NVIDIAReplayMonitor.exe --doctor` |
| 常駐・自動起動の解除と通知設定の復元 | `.\NVIDIAReplayMonitor.exe --uninstall` |

手動でインスタントリプレイをオフにした場合も自動復帰します。オフのままにする場合は監視を停止してください。DRMによる録画制限中は録画できません。

## 更新

1. `--stop` で監視を停止する。
2. [Releases](https://github.com/htomoya16/NVIDIAReplayMonitor/releases)から新しいexeとuninstall.cmdをダウンロードし、同じ場所に上書きする。
3. exeまたはスタートアップのショートカットから起動する。

## 削除

1. NVIDIA Appの録画・インスタントリプレイをオフにする。
2. 配置した `uninstall.cmd` をダブルクリックする。
3. 完了表示を確認し、Enterキーで閉じる。

監視停止・通知設定の復元・自動起動解除後、exeとuninstall.cmdを削除します。同梱READMEは内容が変更されていなければ削除します。フォルダと他のファイルは残ります。

`--uninstall` を直接実行した場合は、配布ファイルは残ります。削除が完了するまで配置フォルダを手動で削除しないでください。

## 問題がある場合

1. `--doctor` で状態を確認する。
2. NVIDIA Appのオーバーレイとエージェント接続の設定を確認する。

| 記録 | 保存先 |
| --- | --- |
| 診断結果 | `%LOCALAPPDATA%\NVIDIAReplayMonitor\doctor.txt` |
| アンインストール失敗時の詳細 | `%LOCALAPPDATA%\NVIDIAReplayMonitor\uninstall-error.txt` |

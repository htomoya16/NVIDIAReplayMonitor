# NVIDIAReplayMonitor

NVIDIA Appのインスタントリプレイを30秒ごとに確認し、オフなら公式MCPでオンへの復帰を試みるWindows常駐ツールです。起動直後にも確認します。接続・操作に失敗しても終了せず、次の30秒周期で再試行します。

## 動作条件

- Windows 11 x64、対応するNVIDIA GPU。
- MCP対応のNVIDIA App（11.0.9.5xx以降）。MCPアクセスとIn-Game Overlayを有効にしてください。有効化操作にはMCPのstandard以上のアクセスが必要です。
- 利用者側のGo・WSL・AI・Codexは不要です。

DRMの録画制限を回避するものではなく、制限中の復帰は保証しません。手動でオフにした場合も復帰対象になるため、オフのままにしたい場合は監視を停止してください。通常の監視では録画・Desktop Capture等の他の設定を変更しません。

## 導入と自動起動

1. GitHubの **Releases** から `NVIDIAReplayMonitor-<バージョン>-windows-amd64.zip` をダウンロードし、固定のフォルダへ展開します。`Source code` は実行ファイルではありません。
2. exeをダブルクリックすると、コンソールを表示せず監視を開始します。警告抑止も自動設定します。二重起動した場合は追加のプロセスが終了します。
3. 自動起動する場合は、exeのショートカットを作成します。名前を `NVIDIAReplayMonitor` にしてください（実際のファイル名は `NVIDIAReplayMonitor.lnk`）。
4. `Win + R` → `shell:startup` で開いたフォルダへ、そのショートカットを置きます。以後はログイン時に自動起動します。

exeを移動した場合はショートカットも作り直してください。レジストリへの自動起動登録は行いません。

## DRM警告の抑止

Netflix等を開いている間の復帰試行で表示される警告だけを抑止します。初回の有効化前にOverlayの正常終了・設定変更・再起動を自動で行います。設定済みなら、以後の監視ではDB操作やOverlay再起動を行いません。録画・インスタントリプレイが動作中の場合は中断せず、オフになってから初期設定します。

この機能は公式MCPに変更APIがないため、NVIDIA内部の通知設定DBを扱います。NVIDIA App **11.0.9.585 / Chromium 128** の形式で検証しています。対応しない形式では変更しません。通知全体やDRMによる録画制限は変更しません。

初期設定に失敗しても常駐を続け、次の30秒周期で再試行します。初期設定が終わるまでリプレイの有効化は見送ります。`--doctor` で状況を確認してください。正常終了できない状態が続く場合は、監視を `--stop` で停止し、NVIDIA AppのOverlayを手動でオフにしてexeを起動後、Overlayをオンに戻してください。強制終了は行いません。

変更前の値と、設定が存在しなかったことも `%LOCALAPPDATA%\NVIDIAReplayMonitor\warning-state.json` に保存します。元DBのバックアップも復元完了まで保持します。**削除が完了するまでは復元情報を消さないでください。** 既に警告が抑止されていた場合は変更せず、復元対象にも加えません。

## 操作・更新・削除

exeを置いたフォルダでPowerShellを開いて実行します。

```powershell
.\NVIDIAReplayMonitor.exe --doctor
.\NVIDIAReplayMonitor.exe --stop
.\NVIDIAReplayMonitor.exe --uninstall
```

| オプション | 動作 |
| --- | --- |
| なし | 警告抑止を自動設定し、30秒固定で監視 |
| `--doctor` | 状態を変更せず、MCP接続・必要ツール・リプレイ状態・自動起動登録を確認 |
| `--stop` | 監視を終了。リプレイのオン・オフは変更しない |
| `--uninstall` | 監視停止・警告設定の復元後、スタートアップの `NVIDIAReplayMonitor.lnk` を削除 |
| `--version` | バージョンを表示 |
| `--licenses <ファイル>` | exeに同梱した依存ライブラリのライセンスを出力 |

操作結果は既存のターミナル、またはダイアログに表示します。`--doctor` の結果は `%LOCALAPPDATA%\NVIDIAReplayMonitor\doctor.txt` に上書き保存します。任意の場所へ保存する場合は次のように指定します（ダイアログなし）。通常の監視中はログを出力しません。

```powershell
Start-Process .\NVIDIAReplayMonitor.exe -ArgumentList '--doctor --output doctor.txt' -Wait
Get-Content .\doctor.txt
```

接続できない場合はNVIDIA AppのMCPアクセス・Overlayを確認してください。`503 Service Unavailable` はMCPのHTTP経路が利用できない状態です。状態フィールドの欠落はオフと扱わず、その周期の変更を見送ります。

- **更新**：`--stop` で終了後、同じ場所のexeを新しいバージョンで置き換えて起動します。
- **削除**：録画・インスタントリプレイをオフにし、同梱の `uninstall.cmd` をダブルクリックします（`--uninstall` でも同じ処理）。常駐停止・警告設定の復元・自動起動解除まで行います。exe・cmd・READMEは残るので、不要ならそれぞれ削除してください。診断結果の `%LOCALAPPDATA%\NVIDIAReplayMonitor\doctor.txt` も削除完了後に削除できます。
- 復元できない場合は自動起動登録を残してエラーを表示するので、案内に従って再実行してください。警告設定だけを復元し、その後に変更した他の設定は保持します。ショートカットを別の名前にした場合は手動で削除してください。
- ショートカット削除だけでは、現在動作中の監視は終了しません。`--stop` だけなら、次回ログイン時の自動起動は残ります。

## ビルド・リリース

開発にはWindowsとGo 1.25以降を使用します。[公式MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)、[golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys/windows)、[純GoのLevelDB](https://github.com/syndtr/goleveldb)を使用します。実行時の本体は単一exeで、検証用ブラウザやGo環境は同梱しません。

```powershell
.\scripts\build.ps1 -Version 0.1.0
```

テスト・`go vet`・依存ライセンスの同梱後、`dist/` にexe・uninstall.cmd・READMEを含むZIPと `SHA256SUMS.txt` を生成します。生成物はGit管理対象外です。`internal/notices` は著作権・依存ライセンスをexeに埋め込むための開発用フォルダで、利用者が別途配置する必要はありません。実機では診断、通常起動での警告抑止、停止、アンインストール後の復元も確認してください。

GitHubリポジトリと `origin` を設定し、変更をコミットした後の公開手順です。

```powershell
git tag v0.1.0
git push origin main v0.1.0
gh release create v0.1.0 dist/NVIDIAReplayMonitor-0.1.0-windows-amd64.zip dist/SHA256SUMS.txt --verify-tag --draft --title v0.1.0 --generate-notes
```

GitHubのReleasesで下書きの添付ファイル・変更内容を確認して公開します。初期の試用版にはPre-releaseを設定します。本プロジェクト自体のライセンスは公開前に決定してください。依存ライブラリのライセンスはexeから取り出せます。

NVIDIAの操作仕様は[公式nvidia-app-skills](https://github.com/NVIDIA/nvidia-app-skills)を参照しています。本ツールはNVIDIA公式製品ではありません。

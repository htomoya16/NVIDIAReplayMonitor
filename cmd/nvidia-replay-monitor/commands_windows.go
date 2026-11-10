package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"nvidiareplaymonitor/internal/infra/winapp"
	"nvidiareplaymonitor/internal/notices"
)

type options struct {
	stop, uninstall, doctor, version, help bool
	output, licenses                       string
}

func parseOptions(args []string) (options, error) {
	var opts options
	flags := flag.NewFlagSet("NVIDIAReplayMonitor", flag.ContinueOnError)
	var output bytes.Buffer
	flags.SetOutput(&output)
	flags.BoolVar(&opts.stop, "stop", false, "監視を停止")
	flags.BoolVar(&opts.uninstall, "uninstall", false, "監視停止と自動起動ショートカット削除")
	flags.BoolVar(&opts.doctor, "doctor", false, "環境・接続状態を診断（状態は変更しません）")
	flags.StringVar(&opts.output, "output", "", "診断・停止・削除の結果を保存し、ダイアログを表示しません")
	flags.StringVar(&opts.licenses, "licenses", "", "依存ライブラリのライセンスを指定ファイルへ出力")
	flags.BoolVar(&opts.version, "version", false, "バージョンを表示")
	flags.BoolVar(&opts.help, "help", false, "操作方法を表示")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			opts.help = true
		} else {
			return opts, errors.New(output.String())
		}
	}
	if flags.NArg() != 0 {
		return opts, errors.New("不明な引数です。--helpを参照してください")
	}
	actions := 0
	for _, enabled := range []bool{opts.stop, opts.uninstall, opts.doctor, opts.version, opts.help, opts.licenses != ""} {
		if enabled {
			actions++
		}
	}
	if actions > 1 {
		return opts, errors.New("操作オプションは1つだけ指定してください")
	}
	if opts.output != "" && !opts.doctor && !opts.stop && !opts.uninstall {
		return opts, errors.New("--outputは--doctor・--stop・--uninstallと組み合わせてください")
	}
	return opts, nil
}

func runCommand(opts options) (bool, int) {
	if opts.uninstall {
		// Setup owns Maintenance while it runs. Request cancellation before
		// waiting for that gate; otherwise the first uninstall is rejected.
		if _, err := winapp.StopForUninstall(); err != nil {
			return commandResult(opts, err.Error(), true)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		release, err := winapp.WaitMaintenance(ctx)
		cancel()
		if err != nil {
			return commandResult(opts, err.Error(), true)
		}
		defer release()
	}
	var message string
	var err error
	switch {
	case opts.help:
		message = "NVIDIAReplayMonitor " + version + "\n\n引数なし: 警告抑止を自動設定し、30秒ごとに監視\n--stop: 監視停止\n--uninstall: 監視停止・警告設定の復元・自動起動ショートカット削除\n--doctor: 状態を変更せず診断\n--doctor --output <ファイル>: 診断結果を保存\n--version: バージョン\n--licenses <ファイル>: 依存ライセンスを出力"
	case opts.version:
		message = fmt.Sprintf("NVIDIAReplayMonitor %s (%s/%s)", version, runtime.GOOS, runtime.GOARCH)
	case opts.licenses != "":
		err = os.WriteFile(opts.licenses, []byte(notices.Text), 0600)
		message = "ライセンスを出力しました: " + opts.licenses
	case opts.doctor:
		report, healthy := buildDoctorReport()
		path := opts.output
		if path == "" {
			path, err = winapp.WriteDoctorReport(report)
		} else {
			err = os.WriteFile(path, []byte(report), 0600)
		}
		if err != nil {
			showMessage(fmt.Sprintf("診断結果を保存できません: %v", err), true)
			return true, 1
		}
		if opts.output == "" {
			showMessage(report+"\n保存先: "+path, !healthy)
		}
		if !healthy {
			return true, 1
		}
		return true, 0
	case opts.uninstall:
		var warning error
		warning, err = uninstall(winapp.StopMonitor, restoreWarning, winapp.RemoveStartupShortcut)
		if err == nil {
			err = removeManualControlState()
		}
		if err == nil {
			err = winapp.RemoveDiagnosticFiles()
		}
		message = "監視停止、警告設定の復元、自動起動ショートカットの解除が完了しました。"
		if warning != nil {
			message += "\n\n注意: " + warning.Error()
		}
	case opts.stop:
		var stopped bool
		stopped, err = winapp.StopMonitor()
		if stopped {
			message = "監視を停止しました。"
		} else {
			message = "監視は起動していません。"
		}
	default:
		return false, 0
	}
	if err != nil {
		return commandResult(opts, err.Error(), true)
	}
	return commandResult(opts, message, false)
}

// Installers use a UTF-8 result file rather than invisible GUI dialogs or
// locale-dependent console output. A failed result write is itself a failure.
func commandResult(opts options, message string, failure bool) (bool, int) {
	if opts.output != "" {
		if err := os.WriteFile(opts.output, []byte(message), 0600); err != nil {
			fmt.Fprintln(os.Stderr, "操作結果を保存できません:", err)
			return true, 1
		}
	} else {
		showMessage(message, failure)
	}
	if failure {
		return true, 1
	}
	return true, 0
}

// Restoration must succeed before removing startup or announcing completion.
func uninstall(stop func() (bool, error), restore func() error, remove func() (bool, error)) (warning error, err error) {
	if _, err := stop(); err != nil {
		return nil, err
	}
	if err := restore(); err != nil {
		var restartWarning *winapp.OverlayRestartWarning
		if !errors.As(err, &restartWarning) {
			return nil, fmt.Errorf("削除は完了していません。自動起動登録は削除していません。\n詳細を確認し、復元情報を削除せず再実行してください。\n%w", err)
		}
		warning = restartWarning
	}
	_, err = remove()
	return warning, err
}

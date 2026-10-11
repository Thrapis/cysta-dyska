package main

import (
	"context"
	"embed"
	"os/exec"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"cysta-dyska/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

// wailsEmitter forwards app events to the frontend once the runtime is up.
type wailsEmitter struct{ ctx context.Context }

func (e *wailsEmitter) Emit(name string, data any) {
	if e.ctx != nil {
		runtime.EventsEmit(e.ctx, name, data)
	}
}

// Shell exposes native OS dialogs to the frontend.
type Shell struct{ ctx context.Context }

// PickFolder opens a native folder picker. Returns "" if the user cancels.
func (s *Shell) PickFolder() (string, error) {
	return runtime.OpenDirectoryDialog(s.ctx, runtime.OpenDialogOptions{})
}

// Reveal shows path selected in File Explorer.
func (s *Shell) Reveal(path string) error {
	cmd, err := app.RevealCommand(path)
	if err != nil {
		return err
	}
	return start(cmd)
}

// OpenSettings opens a Windows Settings page (ms-settings: URIs only).
func (s *Shell) OpenSettings(uri string) error {
	cmd, err := app.SettingsCommand(uri)
	if err != nil {
		return err
	}
	return start(cmd)
}

// start launches cmd without waiting for it.
func start(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func main() {
	emitter := &wailsEmitter{}
	shell := &Shell{}
	a := app.New(emitter)

	err := wails.Run(&options.App{
		Title:     "Čysta Dyska",
		Width:     1100,
		Height:    760,
		MinWidth:  720,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: func(ctx context.Context) {
			emitter.ctx = ctx
			shell.ctx = ctx
		},
		Bind: []interface{}{
			a,
			shell,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

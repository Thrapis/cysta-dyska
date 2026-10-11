package main

import (
	"context"
	"embed"
	"os"
	"os/exec"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"cysta-dyska/internal/app"
	"cysta-dyska/internal/fastscan"
	"cysta-dyska/internal/store"
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
	// The elevated fast-scan helper: read the MFT, write the result, exit.
	// It must not open a window or take the single-instance lock.
	if fastscan.IsHelperCall(os.Args) {
		os.Exit(fastscan.RunHelper(os.Args[2:]))
	}

	emitter := &wailsEmitter{}
	shell := &Shell{}
	history := openHistory()
	a := app.New(emitter, history)

	err := wails.Run(&options.App{
		Title:     "Čysta Dyska",
		Width:     1100,
		Height:    760,
		MinWidth:  720,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// One window only: two would share the history database and could
		// act on the same folders at once.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "cysta-dyska-6f1c2a0e",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if emitter.ctx != nil {
					runtime.WindowUnminimise(emitter.ctx)
					runtime.Show(emitter.ctx)
				}
			},
		},
		OnStartup: func(ctx context.Context) {
			emitter.ctx = ctx
			shell.ctx = ctx
			// Runs only in the instance holding the lock, so a "running"
			// log entry was left by a run that ended mid-action.
			if history != nil {
				if _, err := history.MarkInterrupted(); err != nil {
					println("action log:", err.Error())
				}
			}
		},
		OnShutdown: func(context.Context) {
			if history != nil {
				history.Close()
			}
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

// openHistory opens the scan history database. Without it the app still
// works; it just cannot compare scans.
func openHistory() *store.Store {
	path, err := store.DefaultPath()
	if err == nil {
		var s *store.Store
		if s, err = store.Open(path); err == nil {
			return s
		}
	}
	println("history unavailable:", err.Error())
	return nil
}

// Command dsh-mygo is the native desktop client: a MyGo window that drives a
// DeepSeek Harness host process over newline-delimited JSON-RPC.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	// Ship the Simplified Chinese text of MyGo's own controls, such as the
	// code-copy button; the catalog is linked only when the app imports it.
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"

	"dsh-mygo/internal/app"
	"dsh-mygo/internal/view"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, " ") }

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	opts := startupOptions{}
	bindStartupFlags(flag.CommandLine, &opts)
	flag.Parse()
	if err := validateStartup(opts); err != nil {
		log.Fatal(err)
	}
	mygo.App.SetName("dsh-mygo")
	plan, err := prepareStartup(opts, useHostDefaultRoute(flag.CommandLine))
	if err != nil {
		log.Fatal(err)
	}
	mygo.App.SetLanguages("en-US", "zh-CN")
	mygo.App.SetLocale("en-US")

	var window atomic.Pointer[mygo.Window]
	application := app.New(app.Options{
		ApplyTheme:             func(value string) { mygo.Theme.SetSource(mygo.ThemeSource(value)) },
		ApplyLocale:            mygo.App.SetLocale,
		Launch:                 plan.launch,
		Runtimes:               plan.runtimeManager,
		Profile:                opts.profile,
		DSHHome:                plan.dshHome,
		PrepareProfile:         plan.prepareProfile,
		CWD:                    plan.cwd,
		RequireWorkspace:       plan.requireWorkspace,
		Provider:               opts.provider,
		Model:                  opts.model,
		ReasoningEffort:        opts.effort,
		UseHostDefaults:        plan.useHostDefaults,
		SessionID:              plan.sessionID,
		SDKProtocol:            opts.sdkProtocol,
		MaxFrame:               opts.maxFrame,
		Preflight:              plan.preflight,
		PreflightSummary:       plan.preflightSummary,
		LogDir:                 plan.logDir,
		PreferencesPath:        plan.statePath,
		PreferencesUnavailable: plan.preferencesUnavailable,
		Quit:                   mygo.App.Quit,
		Post: func(fn func()) {
			if win := window.Load(); win != nil {
				win.Update(fn)
			}
		},
	})
	uiModel := application.Model()

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:          "dsh-mygo",
			Width:          1280,
			Height:         820,
			MinWidth:       560,
			MinHeight:      420,
			TitleBarStyle:  mygo.TitleBarHidden,
			TitleBarHeight: int(view.WindowTitleBarHeight),
			StateKey:       "main-v2",
			Content: ui.View(func(c *ui.Context) {
				view.View(c, uiModel)
			}),
		})
		window.Store(win)
		// Drops outside the composer still attach: the window sees what no
		// element took and hands the paths to the same bounded loader.
		win.OnFileDrop(func(e *mygo.FileDropEvent) {
			application.AttachFiles(e.Paths)
		})
		win.OnClose(func(e *mygo.CloseEvent) {
			if !application.ReadyToQuit() {
				e.PreventDefault()
				application.RequestQuit(false)
			}
		})
		go application.Start()
	})
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !application.ReadyToQuit() {
			e.PreventDefault()
			application.RequestQuit(false)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	application.Shutdown(2 * time.Second)
}

func resolveDSHHome(value string) (string, error) {
	if value == "" {
		value = os.Getenv("DSH_HOME")
	}
	if value == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve DSH_HOME: %w", err)
		}
		value = filepath.Join(home, ".dsh")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve DSH_HOME: %w", err)
	}
	return absolute, nil
}

func resolveCWD(value string) (string, error) {
	if value == "" {
		current, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
		value = current
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return absolute, nil
}

// A Start Menu launch must not turn the installation directory into a project.
// Development and explicit CLI launches retain their existing directory rules.
func resolveStartupCWD(value string, requireWorkspace bool) (string, bool, error) {
	required := requireWorkspace && value == ""
	if required {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false, err
		}
		value = home
	}
	cwd, err := resolveCWD(value)
	return cwd, required, err
}

func useHostDefaultRoute(flags *flag.FlagSet) bool {
	useDefaults := true
	flags.Visit(func(option *flag.Flag) {
		if option.Name == "provider" || option.Name == "model" || option.Name == "effort" {
			useDefaults = false
		}
	})
	return useDefaults
}

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
	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/host"
	"dsh-mygo/internal/preferences"
	"dsh-mygo/internal/view"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, " ") }

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	var hostArgs stringList
	flag.Var(&hostArgs, "host-arg", "argument for -host-command (repeatable)")

	dshDir := flag.String("dsh-dir", os.Getenv("DSH_MYGO_DSH_DIR"), "DeepSeek Harness checkout; runs apps/cli/src/bin.ts through tsx")
	profile := flag.String("profile", "mygo", "DSH profile")
	sdkProtocol := flag.Bool("sdk-protocol", false, "use the legacy SDK protocol (requires -profile sdk)")
	provider := flag.String("provider", "deepseek-official", "explicit model provider override (otherwise use Host defaults when supported)")
	model := flag.String("model", "deepseek-flash", "explicit model id override (otherwise use Host defaults when supported)")
	effort := flag.String("effort", "", "optional reasoning effort")
	cwdFlag := flag.String("cwd", "", "session working directory (default: current directory)")
	sessionID := flag.String("session", "", "legacy SDK session id (default: generated; requires -sdk-protocol)")
	dshHome := flag.String("dsh-home", "", "optional DSH_HOME override")
	maxFrame := flag.Int("max-frame", channel.DefaultMaxFrameBytes, "maximum JSON-RPC frame bytes")
	hostCommand := flag.String("host-command", "", "override the host executable")
	logDirFlag := flag.String("log-dir", "", "override the logs directory (testing)")
	stateDirFlag := flag.String("ui-state-dir", "", "override the device-local editing state directory")
	flag.Parse()
	mygo.App.SetName("dsh-mygo")
	useHostDefaults := useHostDefaultRoute(flag.CommandLine)
	if *profile == "sdk" && !*sdkProtocol {
		log.Fatal("-profile sdk requires -sdk-protocol")
	}
	if *sessionID != "" && !*sdkProtocol {
		log.Fatal("-session requires -sdk-protocol; mygo session ids are assigned by the host")
	}

	cwd, err := resolveCWD(*cwdFlag)
	if err != nil {
		log.Fatal(err)
	}
	resourcesDir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		log.Fatal(fmt.Errorf("resolve resources: %w", err))
	}
	if *logDirFlag != "" {
		absolute, err := filepath.Abs(*logDirFlag)
		if err != nil {
			log.Fatal(fmt.Errorf("resolve log directory: %w", err))
		}
		mygo.App.SetPath(mygo.PathLogs, absolute)
	}
	logDir, logErr := mygo.App.Path(mygo.PathLogs)
	if logErr != nil {
		log.Printf("resolve logs: %v", logErr)
	}
	dshHomePath, err := resolveDSHHome(*dshHome)
	if err != nil {
		log.Fatal(err)
	}
	if *sdkProtocol && *sessionID == "" {
		*sessionID = fmt.Sprintf("mygo-%d", time.Now().UnixNano())
	}
	launch, launchErr := host.ResolveLaunch(host.LaunchOptions{
		DshDir:       *dshDir,
		Profile:      *profile,
		CWD:          cwd,
		Command:      *hostCommand,
		Args:         hostArgs,
		ResourcesDir: resourcesDir,
		DSHHome:      dshHomePath,
	})
	preflight := func() error {
		if launchErr != nil {
			return launchErr
		}
		if *sdkProtocol || *hostCommand != "" || *profile != "mygo" {
			return nil
		}
		return host.EnsureProfile(dshHomePath, filepath.Join(resourcesDir, "host"), *profile)
	}
	preflightSummary := "Could not start the DeepSeek Harness host"
	if launchErr == nil && !*sdkProtocol && *hostCommand == "" && *profile == "mygo" {
		preflightSummary = "Could not prepare the DeepSeek Harness profile"
	}

	var window atomic.Pointer[mygo.Window]
	stateRoot, stateErr := mygo.App.Path(mygo.PathUserData)
	if *stateDirFlag != "" {
		stateRoot, stateErr = filepath.Abs(*stateDirFlag)
		if stateErr == nil {
			mygo.App.SetPath(mygo.PathUserData, stateRoot)
		}
	}
	mygo.App.SetLanguages("en-US", "zh-CN")
	mygo.App.SetLocale("en-US")
	statePath := ""
	if stateErr == nil && !*sdkProtocol {
		statePath = preferences.StatePath(stateRoot, dshHomePath, *profile)
	}
	application := app.New(app.Options{
		ApplyTheme:             func(value string) { mygo.Theme.SetSource(mygo.ThemeSource(value)) },
		ApplyLocale:            mygo.App.SetLocale,
		Launch:                 launch,
		CWD:                    cwd,
		Provider:               *provider,
		Model:                  *model,
		ReasoningEffort:        *effort,
		UseHostDefaults:        useHostDefaults,
		SessionID:              *sessionID,
		SDKProtocol:            *sdkProtocol,
		MaxFrame:               *maxFrame,
		Preflight:              preflight,
		PreflightSummary:       preflightSummary,
		LogDir:                 logDir,
		PreferencesPath:        statePath,
		PreferencesUnavailable: stateErr != nil && !*sdkProtocol,
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
			Title:     "dsh-mygo",
			Width:     960,
			Height:    760,
			MinWidth:  560,
			MinHeight: 420,
			StateKey:  "main",
			Content: ui.View(func(c *ui.Context) {
				view.View(c, uiModel)
			}),
		})
		window.Store(win)
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

func useHostDefaultRoute(flags *flag.FlagSet) bool {
	useDefaults := true
	flags.Visit(func(option *flag.Flag) {
		if option.Name == "provider" || option.Name == "model" || option.Name == "effort" {
			useDefaults = false
		}
	})
	return useDefaults
}

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
	"github.com/egoist/mygo/ui"

	"dsh-mygo/internal/app"
	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/host"
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
	flag.Parse()
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
	if *sdkProtocol && *sessionID == "" {
		*sessionID = fmt.Sprintf("mygo-%d", time.Now().UnixNano())
	}
	launch, err := host.ResolveLaunch(host.LaunchOptions{
		DshDir:  *dshDir,
		Profile: *profile,
		CWD:     cwd,
		Command: *hostCommand,
		Args:    hostArgs,
		DSHHome: *dshHome,
	})
	if err != nil {
		log.Fatal(err)
	}

	var window atomic.Pointer[mygo.Window]
	application := app.New(app.Options{
		Launch:          launch,
		CWD:             cwd,
		Provider:        *provider,
		Model:           *model,
		ReasoningEffort: *effort,
		UseHostDefaults: useHostDefaults,
		SessionID:       *sessionID,
		SDKProtocol:     *sdkProtocol,
		MaxFrame:        *maxFrame,
		Post: func(fn func()) {
			if win := window.Load(); win != nil {
				win.Update(fn)
			}
		},
	})
	uiModel := application.Model()

	mygo.App.SetName("dsh-mygo")
	mygo.App.WhenReady(func() {
		window.Store(mygo.NewWindow(mygo.WindowOptions{
			Title:     "dsh-mygo",
			Width:     960,
			Height:    760,
			MinWidth:  560,
			MinHeight: 420,
			StateKey:  "main",
			Content: ui.View(func(c *ui.Context) {
				view.View(c, uiModel)
			}),
		}))
		go application.Start()
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		application.Shutdown(5 * time.Second)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	application.Shutdown(2 * time.Second)
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

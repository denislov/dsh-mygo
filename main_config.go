package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/host"
	"dsh-mygo/internal/preferences"
	"dsh-mygo/internal/runtimes"
	"dsh-mygo/internal/version"
)

// launchMode is the resolved composition root for one CLI invocation. Explicit
// SDK and direct launches never fall through to the device runtime store.
type launchMode int

const (
	launchSDK launchMode = iota
	launchDirect
	launchRuntimeStore
)

type startupOptions struct {
	hostArgs    stringList
	dshDir      string
	profile     string
	sdkProtocol bool
	provider    string
	model       string
	effort      string
	cwd         string
	sessionID   string
	dshHome     string
	maxFrame    int
	hostCommand string
	runtimeHome string
	runtimeDir  string
	logDir      string
	stateDir    string
}

func bindStartupFlags(fs *flag.FlagSet, opts *startupOptions) {
	fs.Var(&opts.hostArgs, "host-arg", "argument for -host-command (repeatable)")
	fs.StringVar(&opts.dshDir, "dsh-dir", os.Getenv("DSH_MYGO_DSH_DIR"), "DeepSeek Harness checkout; runs apps/cli/src/bin.ts through tsx")
	fs.StringVar(&opts.profile, "profile", "mygo", "DSH profile")
	fs.BoolVar(&opts.sdkProtocol, "sdk-protocol", false, "use the legacy SDK protocol (requires -profile sdk)")
	fs.StringVar(&opts.provider, "provider", "deepseek-official", "explicit model provider override (otherwise use Host defaults when supported)")
	fs.StringVar(&opts.model, "model", "deepseek-flash", "explicit model id override (otherwise use Host defaults when supported)")
	fs.StringVar(&opts.effort, "effort", "", "optional reasoning effort")
	fs.StringVar(&opts.cwd, "cwd", "", "session working directory (default: current directory)")
	fs.StringVar(&opts.sessionID, "session", "", "legacy SDK session id (default: generated; requires -sdk-protocol)")
	fs.StringVar(&opts.dshHome, "dsh-home", "", "optional DSH_HOME override")
	fs.IntVar(&opts.maxFrame, "max-frame", channel.DefaultMaxFrameBytes, "maximum JSON-RPC frame bytes")
	fs.StringVar(&opts.hostCommand, "host-command", "", "override the host executable")
	fs.StringVar(&opts.runtimeHome, "runtime-home", "", "override the device-local runtime store (testing)")
	fs.StringVar(&opts.runtimeDir, "runtime-dir", "", "use an extracted runtime bundle directly (development/smoke)")
	fs.StringVar(&opts.logDir, "log-dir", "", "override the logs directory (testing)")
	fs.StringVar(&opts.stateDir, "ui-state-dir", "", "override the device-local editing state directory")
}

func validateStartup(opts startupOptions) error {
	if opts.profile == "sdk" && !opts.sdkProtocol {
		return fmt.Errorf("-profile sdk requires -sdk-protocol")
	}
	if opts.sessionID != "" && !opts.sdkProtocol {
		return fmt.Errorf("-session requires -sdk-protocol; mygo session ids are assigned by the host")
	}
	return nil
}

func resolveLaunchMode(opts startupOptions) launchMode {
	switch {
	case opts.sdkProtocol:
		return launchSDK
	case opts.hostCommand != "" || opts.dshDir != "" || opts.profile != "mygo":
		return launchDirect
	default:
		return launchRuntimeStore
	}
}

type startupPlan struct {
	mode                   launchMode
	useHostDefaults        bool
	cwd                    string
	requireWorkspace       bool
	dshHome                string
	logDir                 string
	stateRoot              string
	statePath              string
	stateErr               error
	preferencesUnavailable bool
	sessionID              string
	launch                 host.Config
	launchErr              error
	preflight              func() error
	preflightSummary       string
	runtimeManager         *runtimes.Manager
	prepareProfile         func(string) error
}

// prepareStartup resolves paths and the launch composition without creating a
// window or starting the Host. Slow I/O and process waits stay in app.Start.
func prepareStartup(opts startupOptions, useHostDefaults bool) (startupPlan, error) {
	plan := startupPlan{mode: resolveLaunchMode(opts), useHostDefaults: useHostDefaults}
	resourcesDir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return startupPlan{}, fmt.Errorf("resolve resources: %w", err)
	}
	devOverrides := opts.sdkProtocol || opts.hostCommand != "" || opts.dshDir != "" || opts.runtimeDir != ""
	packaged := !mygo.IsDev()
	plan.cwd, plan.requireWorkspace, err = resolveStartupCWD(opts.cwd, packaged && !devOverrides)
	if err != nil {
		return startupPlan{}, err
	}
	if opts.logDir != "" {
		absolute, err := filepath.Abs(opts.logDir)
		if err != nil {
			return startupPlan{}, fmt.Errorf("resolve log directory: %w", err)
		}
		mygo.App.SetPath(mygo.PathLogs, absolute)
	}
	plan.logDir, _ = mygo.App.Path(mygo.PathLogs)
	plan.dshHome, err = resolveDSHHome(opts.dshHome)
	if err != nil {
		return startupPlan{}, err
	}
	sessionID := opts.sessionID
	if opts.sdkProtocol && sessionID == "" {
		sessionID = fmt.Sprintf("mygo-%d", time.Now().UnixNano())
	}

	if plan.mode == launchRuntimeStore {
		storeRoot := ""
		if opts.runtimeHome != "" {
			storeRoot, err = filepath.Abs(opts.runtimeHome)
			if err != nil {
				return startupPlan{}, fmt.Errorf("resolve runtime home: %w", err)
			}
		} else {
			cacheRoot, cacheErr := mygo.App.Path(mygo.PathCache)
			if cacheErr != nil {
				return startupPlan{}, fmt.Errorf("resolve runtime cache: %w", cacheErr)
			}
			storeRoot = filepath.Join(cacheRoot, "runtimes")
		}
		plan.runtimeManager = runtimes.NewManager(storeRoot, resourcesDir, version.Repository)
		if opts.runtimeDir != "" {
			plan.runtimeManager.DirectDir, err = filepath.Abs(opts.runtimeDir)
			if err != nil {
				return startupPlan{}, fmt.Errorf("resolve runtime directory: %w", err)
			}
		}
		plan.prepareProfile = func(bundleDir string) error {
			return host.EnsureProfile(plan.dshHome, bundleDir, opts.profile)
		}
	} else {
		plan.launch, plan.launchErr = host.ResolveLaunch(host.LaunchOptions{
			DshDir:       opts.dshDir,
			Profile:      opts.profile,
			CWD:          plan.cwd,
			Command:      opts.hostCommand,
			Args:         opts.hostArgs,
			ResourcesDir: resourcesDir,
			RuntimeDir:   opts.runtimeDir,
			DSHHome:      plan.dshHome,
		})
		plan.preflight = func() error {
			if plan.launchErr != nil {
				return plan.launchErr
			}
			if opts.sdkProtocol || opts.hostCommand != "" || opts.profile != "mygo" {
				return nil
			}
			return host.EnsureProfile(plan.dshHome, filepath.Join(resourcesDir, "host"), opts.profile)
		}
		plan.preflightSummary = "Could not start the DeepSeek Harness host"
		if plan.launchErr == nil && !opts.sdkProtocol && opts.hostCommand == "" && opts.profile == "mygo" {
			plan.preflightSummary = "Could not prepare the DeepSeek Harness profile"
		}
	}

	plan.stateRoot, plan.stateErr = mygo.App.Path(mygo.PathUserData)
	if opts.stateDir != "" {
		plan.stateRoot, plan.stateErr = filepath.Abs(opts.stateDir)
		if plan.stateErr == nil {
			mygo.App.SetPath(mygo.PathUserData, plan.stateRoot)
		}
	}
	plan.preferencesUnavailable = plan.stateErr != nil && !opts.sdkProtocol
	plan.sessionID = sessionID
	if plan.stateErr == nil && !opts.sdkProtocol {
		plan.statePath = preferences.StatePath(plan.stateRoot, plan.dshHome, opts.profile)
	}
	return plan, nil
}

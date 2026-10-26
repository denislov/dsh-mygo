package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestPackagedStartupUsesUserFolderAndHonorsExplicitDirectory(t *testing.T) {
	got, required, err := resolveStartupCWD("", true)
	want, _ := os.UserHomeDir()
	if err != nil || got != want || !required {
		t.Fatalf("packaged startup: %q %v %v", got, required, err)
	}
	explicit := t.TempDir()
	got, required, err = resolveStartupCWD(explicit, true)
	if err != nil || got != explicit || required {
		t.Fatal("explicit folder overridden")
	}
	got, required, err = resolveStartupCWD("", false)
	want, _ = os.Getwd()
	if err != nil || got != want || required {
		t.Fatal("development directory changed")
	}
}

func TestResolveDSHHome(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveDSHHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.Abs(dir); got != want {
		t.Fatalf("explicit home = %q, want %q", got, want)
	}

	t.Setenv("DSH_HOME", dir)
	got, err = resolveDSHHome("")
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.Abs(dir); got != want {
		t.Fatalf("environment home = %q, want %q", got, want)
	}
}

func TestExplicitRouteFlagsOverrideHostDefaults(t *testing.T) {
	for _, scenario := range []struct {
		args []string
		want bool
	}{
		{nil, true},
		{[]string{"-cwd", "work"}, true},
		{[]string{"-model", "deepseek-flash"}, false},
		{[]string{"-provider", "deepseek-official"}, false},
		{[]string{"-effort", ""}, false},
	} {
		flags := flag.NewFlagSet("test", flag.ContinueOnError)
		for _, name := range []string{"provider", "model", "effort", "cwd"} {
			flags.String(name, "", "")
		}
		if err := flags.Parse(scenario.args); err != nil {
			t.Fatal(err)
		}
		if useHostDefaultRoute(flags) != scenario.want {
			t.Fatalf("incorrect route priority for %v", scenario.args)
		}
	}
}

func TestLaunchModePriority(t *testing.T) {
	for _, scenario := range []struct {
		name string
		opts startupOptions
		want launchMode
	}{
		{"device runtime", startupOptions{profile: "mygo"}, launchRuntimeStore},
		{"runtime dir stays device-store", startupOptions{profile: "mygo", runtimeDir: "runtime"}, launchRuntimeStore},
		{"host command", startupOptions{profile: "mygo", hostCommand: "node"}, launchDirect},
		{"source checkout", startupOptions{profile: "mygo", dshDir: "D:/dsh"}, launchDirect},
		{"other profile", startupOptions{profile: "custom"}, launchDirect},
		{"sdk wins", startupOptions{profile: "sdk", sdkProtocol: true, hostCommand: "node"}, launchSDK},
	} {
		if got := resolveLaunchMode(scenario.opts); got != scenario.want {
			t.Fatalf("%s: mode = %v, want %v", scenario.name, got, scenario.want)
		}
	}
}

func TestStartupValidation(t *testing.T) {
	if err := validateStartup(startupOptions{profile: "sdk"}); err == nil {
		t.Fatal("profile sdk without -sdk-protocol was accepted")
	}
	if err := validateStartup(startupOptions{profile: "mygo", sessionID: "id"}); err == nil {
		t.Fatal("-session outside SDK was accepted")
	}
	if err := validateStartup(startupOptions{profile: "sdk", sdkProtocol: true, sessionID: "id"}); err != nil {
		t.Fatal(err)
	}
	if err := validateStartup(startupOptions{profile: "mygo"}); err != nil {
		t.Fatal(err)
	}
}

func TestStartupFlagsKeepRepeatedArgsAndExplicitEmptyEffort(t *testing.T) {
	for _, args := range [][]string{
		{"-host-arg", "one", "-host-arg", "two", "-effort", ""},
		{"-host-arg=one", "-host-arg=two", "-effort="},
	} {
		var opts startupOptions
		flags := flag.NewFlagSet("test", flag.ContinueOnError)
		bindStartupFlags(flags, &opts)
		if err := flags.Parse(args); err != nil {
			t.Fatal(err)
		}
		if len(opts.hostArgs) != 2 || opts.hostArgs[0] != "one" || opts.hostArgs[1] != "two" {
			t.Fatalf("host args = %v", opts.hostArgs)
		}
		if useHostDefaultRoute(flags) {
			t.Fatal("explicit empty effort did not override Host defaults")
		}
	}
}

package main

import (
	"flag"
	"path/filepath"
	"testing"
)

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

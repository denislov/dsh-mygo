package main

import (
	"flag"
	"testing"
)

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

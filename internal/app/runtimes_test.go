package app

import (
	"testing"

	"dsh-mygo/internal/runtimes"
	"dsh-mygo/internal/view"
)

func TestStartWithoutRuntimeAwaitsSetup(t *testing.T) {
	manager := runtimes.NewManager(t.TempDir(), t.TempDir(), "denislov/dsh-mygo")
	application := New(Options{
		CWD:      t.TempDir(),
		Runtimes: manager,
		Profile:  "mygo",
		Post:     func(fn func()) { fn() },
	})
	application.Start()
	if !application.model.Runtime.SetupRequired {
		t.Fatal("setup was not required without a runtime")
	}
	if application.model.Phase != view.PhaseAwaitingRuntime {
		t.Fatalf("phase = %v, want awaiting runtime", application.model.Phase)
	}
	if application.model.Runtime.Error == "" {
		t.Fatal("setup error message is empty")
	}
	if application.supervisor != nil {
		t.Fatal("host started without a runtime")
	}
}

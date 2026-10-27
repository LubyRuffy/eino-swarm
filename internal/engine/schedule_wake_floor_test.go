package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// The wake tool tells the model to wait about a third of an estimate, which
// for a short estimate lands under the configured floor. Learning the floor
// from a refusal costs a model call per attempt, so the manager's own copy of
// the tool states the floor in force. A worker's deny stub and the generic
// constructor stay free of it: they have no engine to ask.
func TestManagerWakeToolStatesTheConfiguredMinimumInterval(t *testing.T) {
	e := newTestEngine(t)
	if err := e.cfg.Update(func(c *config.Config) error {
		c.Swarm.ScheduleMinIntervalSeconds = 45
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	th, _ := e.CreateThread("", "", "")
	rt := e.runtimeFor(th.ID)

	info, err := rt.managerScheduleTools(nil)[0].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != ToolScheduleWake {
		t.Fatalf("first manager schedule tool is %q", info.Name)
	}
	want := "at least 45 seconds"
	if !strings.Contains(info.Desc, want) {
		t.Fatalf("manager wake tool does not state the floor %q:\n%s", want, info.Desc)
	}
	if !strings.Contains(info.Desc, "about a third") {
		t.Fatal("stating the floor must not drop the remaining-estimate guidance")
	}

	generic, err := ScheduleWakeTool(nil).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(generic.Desc, "at least") {
		t.Fatalf("the generic constructor invented a floor:\n%s", generic.Desc)
	}
}

// The description is part of the cached request prefix. It must not change
// between two calls of one engine, or every call would miss the cache.
func TestManagerWakeToolDescriptionIsStableAcrossBuilds(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	rt := e.runtimeFor(th.ID)
	var descs [2]string
	for i := range descs {
		info, err := rt.managerScheduleTools(nil)[0].Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		descs[i] = info.Desc
	}
	if descs[0] != descs[1] {
		t.Fatalf("description changed between builds:\n%s\n%s", descs[0], descs[1])
	}
	floor := minSeconds(e.scheduleMinInterval())
	if !strings.Contains(descs[0], fmt.Sprintf("at least %d seconds", floor)) {
		t.Fatalf("default floor %d missing:\n%s", floor, descs[0])
	}
}

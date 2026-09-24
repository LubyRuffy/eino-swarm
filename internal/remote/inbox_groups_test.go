package remote

import (
	"fmt"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestListGivesEachProjectAndRecentsTheirOwnFive(t *testing.T) {
	e := testEngine(t)
	alpha, err := e.CreateProject("alpha", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	beta, err := e.CreateProject("beta", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seed := func(project, title string, n int, at time.Time) {
		t.Helper()
		for i := 0; i < n; i++ {
			th, err := e.CreateThread("", "", project)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Store().UpdateThread(th.ID, map[string]any{
				"title":          fmt.Sprintf("%s-%d", title, i),
				"last_active_at": at.Add(time.Duration(i) * time.Second),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	seed(alpha.ID, "a", 7, now)
	seed(beta.ID, "b", 2, now.Add(time.Hour))
	seed("", "r", 6, now.Add(2*time.Hour))

	listed := Handle(e, config.RemoteConfig{ThreadLimit: 5}, Request{ID: "l", Op: OpList}, "relay", "s")
	if !listed.OK {
		t.Fatalf("%+v", listed)
	}
	// The global page is still five idle rows for a phone that has one More.
	if len(listed.Threads) != 5 || !listed.More {
		t.Fatalf("global page %+v", listed.Threads)
	}
	byID := map[string]ThreadGroup{}
	for _, g := range listed.Groups {
		byID[g.ID] = g
	}
	alphaG, ok := byID[alpha.ID]
	if !ok || len(alphaG.Threads) != 5 || !alphaG.More || alphaG.Next == "" {
		t.Fatalf("alpha %+v", alphaG)
	}
	betaG := byID[beta.ID]
	if len(betaG.Threads) != 2 || betaG.More {
		t.Fatalf("beta %+v", betaG)
	}
	recent := byID[GroupRecent]
	if len(recent.Threads) != 5 || !recent.More {
		t.Fatalf("recent %+v", recent)
	}
	for _, th := range recent.Threads {
		if th.ProjectID != "" {
			t.Fatalf("recent row has a project %+v", th)
		}
	}

	more := Handle(e, config.RemoteConfig{ThreadLimit: 5}, Request{
		ID: "m", Op: OpMore, Group: alpha.ID, Cursor: alphaG.Next,
	}, "relay", "s")
	if !more.OK || len(more.Threads) != 2 || more.More || len(more.Groups) != 0 {
		t.Fatalf("alpha more %+v", more)
	}
	for _, th := range more.Threads {
		if th.ProjectID != alpha.ID {
			t.Fatalf("paged into another folder %+v", th)
		}
	}
}

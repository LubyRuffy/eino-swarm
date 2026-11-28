package app

import (
	"context"
	"net/http"

	"github.com/LubyRuffy/eino-swarm/internal/remote"
	"github.com/LubyRuffy/eino-swarm/internal/update"
)

// ConfigureHostUpdate attaches the portable Linux installer after Listen has
// bound the address the replacement must keep. Existing phone pumps read it
// from their host, including those connected during engine startup.
func (a *App) ConfigureHostUpdate(ctx context.Context, current string, opts update.HostUpdateOptions) {
	a.hostUpdate = update.NewHostService(ctx, a.Config.DataDir(), current, opts, a.Server.HoldHostUpdate, a.Logger)
	a.Remote.SetHostUpdater(a.hostUpdate)
}

// BeginHostUpdateTrial brings up the route used for the original phone's
// round trip. Models, workers, scheduled work and HTTP mutations stay closed.
func (a *App) BeginHostUpdateTrial(verify remote.HostUpdateTrial) {
	a.Remote.SetHostUpdateTrial(verify)
	a.Remote.StartTrialBackground()
}

// StartPromotedHostAccounts runs only after the stable owner commits.
func (a *App) StartPromotedHostAccounts() { a.Remote.StartBackground() }

// EndHostUpdateTrial restores normal work admission after engine recovery.
func (a *App) EndHostUpdateTrial() {
	a.Remote.SetHostUpdateTrial(nil)
	a.hostTrial.Store(false)
}

func (a *App) serveRequest(w http.ResponseWriter, r *http.Request) {
	if a.hostTrial.Load() && (r.Method != http.MethodGet || r.URL.Path != "/api/meta") {
		http.Error(w, "host update verification is in progress", http.StatusConflict)
		return
	}
	a.Server.Handler().ServeHTTP(w, r)
}

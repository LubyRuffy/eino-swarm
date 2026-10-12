package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// A restart launched from Finder has another working directory, so passing
// the original relative flag would open a different conversation database.
func TestDesktopUpdaterReopensTheSameConfiguredInstance(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	relative, err := filepath.Rel(cwd, dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(relative)
	if err != nil {
		t.Fatal(err)
	}
	for _, mock := range []bool{false, true} {
		svc := desktopUpdater(cfg, Options{Version: "1.2.3", DataDir: relative, Mock: mock})
		if svc.Current != "1.2.3" || svc.DataDir != dir || !filepath.IsAbs(svc.DataDir) || svc.Mock != mock {
			t.Fatalf("updater changed the active engine instance: version=%q dataDir=%q mock=%t", svc.Current, svc.DataDir, svc.Mock)
		}
	}
}

package remote

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/pairlink/crypto"
)

func TestLoadOrCreateIdentityPersists(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := loadIdentity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadIdentity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.Fingerprint(a.Public()) != crypto.Fingerprint(b.Public()) {
		t.Fatal("identity was re-minted")
	}
}

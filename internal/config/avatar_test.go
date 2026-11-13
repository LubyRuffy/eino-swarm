package config

import (
	"strings"
	"testing"
)

func TestRememberRemoteAvatarURLReplacesOnlyTheCurrentAccountPicture(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin := "https://gateway.example.invalid"
	cfg.Remote.HubURL = origin
	cfg.Remote.AuthMode = RemoteAuthAccount
	token := strings.Repeat("a", 64)
	user := strings.Repeat("b", 32)
	path, err := cfg.StageRemoteToken(token, origin, "device-key")
	if err != nil {
		t.Fatal(err)
	}
	receipt := RemoteCredential{
		Token: token, Origin: origin, Authorized: true,
		HostID: strings.Repeat("c", 32), UserID: user, Username: "account", DisplayName: "Account",
		AvatarURL: "https://images.example.test/first.png",
	}
	if err := cfg.ActivateRemoteToken(path, receipt); err != nil {
		t.Fatal(err)
	}
	next := "https://images.example.test/next.png?v=1"
	if err := cfg.RememberRemoteAvatarURL(origin, token, user, "  "+next+"  "); err != nil {
		t.Fatal(err)
	}
	saved, err := cfg.ReadRemoteCredential()
	if err != nil || saved.AvatarURL != next || saved.Token != token || saved.DisplayName != "Account" {
		t.Fatalf("picture update changed the receipt: %+v %v", saved, err)
	}
	for _, rejected := range []string{"", "javascript:alert(1)", "https://user:secret@images.example.test/a.png"} {
		if err := cfg.RememberRemoteAvatarURL(origin, token, user, rejected); err != nil {
			t.Fatal(err)
		}
	}
	if err := cfg.RememberRemoteAvatarURL(origin, token, strings.Repeat("d", 32), "https://images.example.test/other.png"); err != nil {
		t.Fatal(err)
	}
	saved, err = cfg.ReadRemoteCredential()
	if err != nil || saved.AvatarURL != next {
		t.Fatalf("rejected address cleared the saved picture: %+v %v", saved, err)
	}
}

func TestAcceptedAvatarURLKeepsAPublicHTTPAddress(t *testing.T) {
	const picture = "https://images.example.test/a.png?sig=a+b&v=1"
	if got := AcceptedAvatarURL("  " + picture + "  "); got != picture {
		t.Fatalf("query string = %q", got)
	}
	if got := AcceptedAvatarURL("http://images.example.test/thumb.jpg"); got != "http://images.example.test/thumb.jpg" {
		t.Fatalf("http address = %q", got)
	}
	long := "https://images.example.test/" + strings.Repeat("a", 2048-len("https://images.example.test/"))
	if len(long) != 2048 || AcceptedAvatarURL(long) != long {
		t.Fatal("a 2048-byte public address must be kept")
	}
}

func TestAcceptedAvatarURLDropsUnsafeAddresses(t *testing.T) {
	long := "https://images.example.test/" + strings.Repeat("a", 2049-len("https://images.example.test/"))
	for _, raw := range []string{
		"",
		"   ",
		"javascript:alert(1)",
		"HTTPS://images.example.test/a.png",
		"https://user:secret@images.example.test/a.png",
		"https://images.example.test/a.png#fragment",
		"https://images.example.test/a b.png",
		"https://images.example.test/a\u00a0b.png",
		"https://images.example.test/a\nb.png",
		"https://images.example.test/a\x00b.png",
		"https://images.example.test/a\\b.png",
		"/a.png",
		long,
	} {
		if got := AcceptedAvatarURL(raw); got != "" {
			t.Fatalf("accepted %q as %q", raw, got)
		}
	}
	// A query is not a fragment. The hash character itself is never an address.
	if got := AcceptedAvatarURL("https://images.example.test/a.png?next=1"); got == "" {
		t.Fatal("a query must remain")
	}
}

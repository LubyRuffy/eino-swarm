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
		AvatarURL: "https://images.fofa.info/first.png",
	}
	if err := cfg.ActivateRemoteToken(path, receipt); err != nil {
		t.Fatal(err)
	}
	next := "https://images.fofa.info/next.png?v=1"
	if err := cfg.RememberRemoteAvatarURL(origin, token, user, "  "+next+"  "); err != nil {
		t.Fatal(err)
	}
	saved, err := cfg.ReadRemoteCredential()
	if err != nil || saved.AvatarURL != next || saved.Token != token || saved.DisplayName != "Account" {
		t.Fatalf("picture update changed the receipt: %+v %v", saved, err)
	}
	for _, rejected := range []string{"", "javascript:alert(1)", "https://user:secret@images.fofa.info/a.png"} {
		if err := cfg.RememberRemoteAvatarURL(origin, token, user, rejected); err != nil {
			t.Fatal(err)
		}
	}
	if err := cfg.RememberRemoteAvatarURL(origin, token, strings.Repeat("d", 32), "https://images.fofa.info/other.png"); err != nil {
		t.Fatal(err)
	}
	saved, err = cfg.ReadRemoteCredential()
	if err != nil || saved.AvatarURL != next {
		t.Fatalf("rejected address cleared the saved picture: %+v %v", saved, err)
	}
}

func TestAcceptedAvatarURLKeepsAPublicHTTPAddress(t *testing.T) {
	const picture = "https://images.fofa.info/a.png?sig=a+b&v=1"
	if got := AcceptedAvatarURL("  " + picture + "  "); got != picture {
		t.Fatalf("query string = %q", got)
	}
	if got := AcceptedAvatarURL("http://images.fofa.info/thumb.jpg"); got != "http://images.fofa.info/thumb.jpg" {
		t.Fatalf("http address = %q", got)
	}
	if got := AcceptedAvatarURL("https://fofa.info/a.png"); got != "https://fofa.info/a.png" {
		t.Fatalf("apex address = %q", got)
	}
	if got := AcceptedAvatarURL("https://CDN.Images.Fofa.Info/a.png"); got != "https://CDN.Images.Fofa.Info/a.png" {
		t.Fatalf("subdomain address = %q", got)
	}
	long := "https://images.fofa.info/" + strings.Repeat("a", 2048-len("https://images.fofa.info/"))
	if len(long) != 2048 || AcceptedAvatarURL(long) != long {
		t.Fatal("a 2048-byte public address must be kept")
	}
}

func TestAcceptedAvatarURLDropsUnsafeAddresses(t *testing.T) {
	long := "https://images.fofa.info/" + strings.Repeat("a", 2049-len("https://images.fofa.info/"))
	for _, raw := range []string{
		"",
		"   ",
		"javascript:alert(1)",
		"HTTPS://images.fofa.info/a.png",
		"https://user:secret@images.fofa.info/a.png",
		"https://images.fofa.info/a.png#fragment",
		"https://images.fofa.info/a b.png",
		"https://images.fofa.info/a\u00a0b.png",
		"https://images.fofa.info/a\nb.png",
		"https://images.fofa.info/a\x00b.png",
		"https://images.fofa.info/a\\b.png",
		"/a.png",
		"https://images.example.test/a.png",
		"https://notfofa.info/a.png",
		"https://fofa.info.evil.com/a.png",
		"https://i.nosec.org/a.png",
		"https://127.0.0.1/a.png",
		long,
	} {
		if got := AcceptedAvatarURL(raw); got != "" {
			t.Fatalf("accepted %q as %q", raw, got)
		}
	}
	// A query is not a fragment. The hash character itself is never an address.
	if got := AcceptedAvatarURL("https://images.fofa.info/a.png?next=1"); got == "" {
		t.Fatal("a query must remain")
	}
}

package remote

import "github.com/LubyRuffy/eino-swarm/internal/config"

// loginAvatar keeps a previously accepted picture when this authorization
// omits the field or supplies an address that cannot be shown. A different
// account does not inherit it.
func loginAvatar(previous config.RemoteCredential, origin, userID, raw string) string {
	if next := config.AcceptedAvatarURL(raw); next != "" {
		return next
	}
	if previous.Authorized && previous.Origin == origin && previous.UserID == userID {
		return config.AcceptedAvatarURL(previous.AvatarURL)
	}
	return ""
}

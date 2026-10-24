package desktop

import (
	"encoding/json"
	"net/url"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Location is independent of paint health: a selected draft must survive a
// renderer failure between two paint probes. No draft content crosses here.
type recoveryLocation struct {
	ThreadID  string `json:"thread_id"`
	TurnID    string `json:"turn_id"`
	ProjectID string `json:"project_id"`
	DraftKey  string `json:"draft_key"`
}

func readRecoveryLocation(message string) (recoveryLocation, bool) {
	var location recoveryLocation
	err := json.Unmarshal([]byte(message), &location)
	return location, err == nil && len(location.ThreadID) <= 128 && len(location.TurnID) <= 128 && len(location.ProjectID) <= 128 && len(location.DraftKey) <= 256
}

func validRecoveryLocation(expected, session string, generation, document int, message string, origin *application.OriginInfo) (recoveryLocation, bool) {
	if origin == nil || !origin.IsMainFrame || !sameDirectoryOrigin(expected, origin.Origin) ||
		(origin.TopOrigin != "" && !sameDirectoryOrigin(expected, origin.TopOrigin)) || len(message) > recoveryMessageLimit {
		return recoveryLocation{}, false
	}
	var receipt struct {
		Type       string `json:"type"`
		Session    string `json:"session"`
		Generation int    `json:"generation"`
		Document   *int   `json:"document"`
	}
	if json.Unmarshal([]byte(message), &receipt) != nil || receipt.Type != "zwai:recovery-location" || receipt.Session != session || receipt.Generation != generation || receipt.Document == nil || *receipt.Document != document {
		return recoveryLocation{}, false
	}
	return readRecoveryLocation(message)
}

func recoveryLocationFromURL(raw string) recoveryLocation {
	u, err := url.Parse(raw)
	if err != nil {
		return recoveryLocation{}
	}
	q := u.Query()
	l := recoveryLocation{ThreadID: q.Get("recovery_thread"), ProjectID: q.Get("recovery_project"), DraftKey: q.Get("recovery_draft")}
	if l.DraftKey == "" && (l.ThreadID != "" || l.ProjectID != "") {
		if l.ThreadID != "" {
			l.DraftKey = "thread:" + l.ThreadID
		} else {
			l.DraftKey = "new:" + l.ProjectID
		}
	}
	return l
}

func (l recoveryLocation) sameScope(other recoveryLocation) bool {
	return l.ThreadID == other.ThreadID && l.ProjectID == other.ProjectID && l.DraftKey == other.DraftKey
}

func (l recoveryLocation) matchesRestored(other recoveryLocation) bool {
	// Like explicitComposerRecovery, thread routing takes precedence over project
	// metadata. Boot learns the actual project from that selected thread.
	if l.ThreadID != "" {
		return l.ThreadID == other.ThreadID && l.DraftKey == other.DraftKey
	}
	return l.sameScope(other)
}

func (l recoveryLocation) windowURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for key, value := range map[string]string{"recovery_thread": l.ThreadID, "recovery_project": l.ProjectID, "recovery_draft": l.DraftKey} {
		q.Del(key)
		if value != "" {
			q.Set(key, value)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

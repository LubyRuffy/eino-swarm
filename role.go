package swarm

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// One worker per role. A model that wants another worker at the same time
// invents a new role rather than reuse the job word, and the invention is
// often a path (job/segment/segment). That path is what the roster prints.
// A slash, a space, or anything that is not a short job name is rewritten
// to the job word before the worker is registered. The next free suffix
// (-2, -3, …) keeps those workers distinct.

const maxRoleRunes = 32

func dirtySpawnRole(role string) bool {
	if role == "" || utf8.RuneCountInString(role) > maxRoleRunes {
		return true
	}
	if strings.ContainsAny(role, "/\\ \t\n\r") {
		return true
	}
	prevSep := false
	for i, r := range role {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			prevSep = false
			continue
		}
		if (r == '-' || r == '_') && i > 0 && !prevSep {
			prevSep = true
			continue
		}
		return true
	}
	return prevSep
}

// spawnStem is the job word at the front of a path-shaped role.
// manager is the host id, so a path that starts there becomes worker.
func spawnStem(role string) string {
	if i := strings.IndexAny(role, "/\\"); i >= 0 {
		role = role[:i]
	}
	var b strings.Builder
	gap := false
	for _, r := range role {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if gap && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			gap = false
			continue
		}
		if b.Len() > 0 {
			gap = true
		}
	}
	stem := strings.TrimRight(b.String(), "-")
	if utf8.RuneCountInString(stem) > maxRoleRunes {
		stem = string([]rune(stem)[:maxRoleRunes])
		stem = strings.TrimRight(stem, "-")
	}
	if stem == "" || stem == DefaultManagerID {
		return "worker"
	}
	return stem
}

// claimReadableRole reserves a free job name. The caller must releaseRoleClaim
// when the spawn returns — the handle (or a refusal) is what keeps the name
// after that. Two path-shaped spawns of the same job must not both observe
// the name as free.
func (r *Registry) claimReadableRole(stem string) string {
	if stem == "" || stem == DefaultManagerID {
		stem = "worker"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pendingRoles == nil {
		r.pendingRoles = map[string]int{}
	}
	pick := stem
	if r.roleBusyLocked(stem) {
		for n := 2; n < 10000; n++ {
			candidate := stem + "-" + strconv.Itoa(n)
			if !r.roleBusyLocked(candidate) {
				pick = candidate
				break
			}
		}
	}
	r.pendingRoles[pick]++
	return pick
}

func (r *Registry) releaseRoleClaim(role string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pendingRoles == nil {
		return
	}
	r.pendingRoles[role]--
	if r.pendingRoles[role] <= 0 {
		delete(r.pendingRoles, role)
	}
}

func (r *Registry) roleBusyLocked(role string) bool {
	if r.pendingRoles[role] > 0 {
		return true
	}
	for _, h := range r.agents {
		if h != nil && h.Role == role {
			return true
		}
	}
	for _, p := range r.past {
		if p != nil && p.Role == role {
			return true
		}
	}
	return false
}

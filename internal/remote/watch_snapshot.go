package remote

import "encoding/json"

// Detail shares the sealed-frame budget with the live edge. Keep the newest
// events and let the existing older-page cursor recover any trimmed rows.
func packWatchDetail(resp Response, detail *ThreadDetail) Response {
	resp.Detail = detail
	for {
		raw, err := json.Marshal(resp)
		if err != nil {
			return fail(resp.ID, resp.Path, resp.SessionID, "", fmtErr(err))
		}
		if len(raw) <= plaintextBudget() {
			return resp
		}
		if len(resp.Events) == 0 {
			return fail(resp.ID, resp.Path, resp.SessionID, "too_large", "conversation metadata exceeds frame budget")
		}
		resp.Events = resp.Events[1:]
		resp.More = true
	}
}

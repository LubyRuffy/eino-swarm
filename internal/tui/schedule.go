package tui

// Schedule tools have no inbox in the terminal. The status line is the
// only place a wait/cancel/findings result can land without a panel.

func scheduleNotice(name, args string) string {
	switch name {
	case "schedule_wake":
		return "A wait is armed."
	case "cancel_schedule":
		return "A wait was cancelled."
	case "report_schedule":
		return reportScheduleNotice(args)
	default:
		return ""
	}
}

func reportScheduleNotice(args string) string {
	obj, ok := parseObject(args)
	if !ok {
		return ""
	}
	raw, present := obj["findings"]
	if !present {
		return ""
	}
	if s := flatten(asText(raw)); s != "" {
		return s
	}
	switch raw.(type) {
	case nil, string:
		return ""
	default:
		// Non-text findings exist, but dumping the envelope would be the
		// JSON blob a status line cannot show.
		return "Scheduled check reported."
	}
}

export const en = {
  "scan.title": "Scan to bind this PC",
  "scan.hint":
    "Open zwai Settings → Phone and point the camera at the pairing QR. Paste is the same URI when there is no camera.",
  "scan.camera": "Scan QR",
  "scan.uri": "Pairing URI",
  "scan.paste": "Paste and bind",
  "scan.showPaste": "Paste URI instead",
  "scan.hidePaste": "Hide paste",
  "scan.retry": "Retry",
  "scan.hostOffline":
    "This PC is not on the hub. Show a new QR after the desktop reconnects.",

  "home.app": "zwai",
  "home.inProgress": "In progress",
  "home.recent": "Recent",
  "home.more": "More",
  "home.unlink": "Unlink",
  "home.project": "Project",
  "home.defaultProject": "Default",
  "home.start": "Start",
  "home.ask": "Waiting for an answer",
  "home.live": "live",
  "home.waiting": "Waiting",
  "home.newMessage": "New message",
  "home.open": "Open {title}",
  "home.relay": "relay",
  "home.direct": "direct",
  "home.offline": "offline",
  "home.reconnecting": "Reconnecting",

  "thread.back": "Back",
  "thread.stop": "Stop",
  "thread.send": "Send",
  "thread.followUp": "Follow-up",
  "thread.steer": "Steer",
  "thread.answer": "Answer",
  "thread.message": "Message",
  "thread.plan": "Planning",
  "thread.image": "Image",
  "thread.running": "running",
  "thread.waiting": "Waiting",
  "thread.earlier": "Earlier",
  "thread.loading": "Loading",
  "thread.tool": "tool",
  "thread.rosterDone": "{n} done",
  "thread.rosterFailed": "{n} failed",
  "thread.rosterRunning": "{n} running",
  "thread.rosterUndelivered": "{n} undelivered",

  "quote.selected": "Selected text",

  "notice.scheduleArmed": "A wait is armed.",
  "notice.scheduleCancelled": "A wait was cancelled.",
  "notice.scheduleFired": "Scheduled check.",

  "goal.pursuing": "Pursuing",
  "goal.done": "Done",
  "goal.paused": "Paused",
  "goal.blocked": "Blocked",
  "goal.failedTurn": "The last turn failed.",
  "goal.start": "Start goal",
  "goal.waitHint":
    "Parked until the next check. This is not an error. Run now or Cancel wait on the wait below.",
  "goal.capHint":
    "Auto-continue paused. This is not an error. Press Start to keep going.",
  "goal.idleHint":
    "Auto-continue paused: the last continuation made no progress. Press Start to keep going.",
  "goal.completeHint": "If this finished too early, press Start to keep going.",

  "schedule.waiting": "Waiting",
  "schedule.nextCheck": "Next check {time}",
  "schedule.runNow": "Run now",
  "schedule.cancel": "Cancel wait",

  "markdown.copyCode": "Copy code",
  "markdown.copyFormula": "Copy formula",
  "markdown.copied": "Copied",

  "ask.title": "A question for you",
  "ask.submit": "Submit",
  "ask.other": "Other",

  "err.reconnect": "Connection lost. Retry to bind again without unlinking.",
  "err.rpc": "The request failed.",
  "err.open": "Could not open that conversation.",
  "err.watch": "Could not follow that conversation.",
  "locale.en": "EN",
  "locale.zh": "中文",
} as const

export type MessageKey = keyof typeof en

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
  "home.newMessage": "New message",
  "home.open": "Open {title}",
  "home.relay": "relay",
  "home.direct": "direct",

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
  "thread.earlier": "Earlier",
  "thread.loading": "Loading",
  "thread.tool": "tool",

  "notice.scheduleArmed": "A wait is armed.",
  "notice.scheduleCancelled": "A wait was cancelled.",
  "notice.scheduleFired": "Scheduled check.",

  "markdown.copyCode": "Copy code",
  "markdown.copyFormula": "Copy formula",
  "markdown.copied": "Copied",

  "ask.title": "A question for you",
  "ask.submit": "Submit",
  "ask.other": "Other",

  "err.reconnect": "Connection lost. Retry to bind again without unlinking.",
  "locale.en": "EN",
  "locale.zh": "中文",
} as const

export type MessageKey = keyof typeof en

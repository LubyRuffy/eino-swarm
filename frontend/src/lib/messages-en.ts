/** English UI chrome. Keys are stable; Chinese lives in messages-zh.ts.
 *  Agent replies and protocol markers (quoted-send wrappers) stay English. */
export const en = {
  "header.idle": "Idle",
  "header.working": "Working · ",
  "header.waiting": "Waiting · ",
  "header.newConversation": "New conversation",
  "header.hideConversations": "Hide conversations",
  "header.showConversations": "Show conversations",
  "header.switchTheme": "Switch theme",
  "header.switchLanguage": "Switch language",
  "header.togglePanel": "Toggle side panel",
  "header.copyTurn": "Copy the turn id — `zwai trace <id>` replays it",
  "header.reconnecting": "Reconnecting",
  "header.lostStreamDesktop":
    "Lost the event stream to the app. It retries on its own and replays anything missed.",
  "header.lostStreamWeb":
    "Lost the event stream to the server. It retries on its own and replays anything missed.",
  "header.workingIn": "Working in {path}",
  "header.languageMark": "中",

  "sidebar.newConversation": "New conversation",
  "sidebar.search": "Search conversations (⌘K)",
  "sidebar.settings": "Settings",
  "sidebar.resize": "Resize the conversation list",
  "sidebar.empty": "No conversations yet. Start one and it will appear here.",
  "sidebar.emptyProject": "No conversations in this project yet.",
  "sidebar.untitled": "Untitled",
  "sidebar.more": "More",
  "sidebar.rename": "Rename",
  "sidebar.delete": "Delete",
  "sidebar.pinned": "Pinned",
  "sidebar.recents": "Recents",
  "sidebar.pin": "Pin",
  "sidebar.unpin": "Unpin",

  "projects.title": "Projects",
  "projects.new": "New project",
  "projects.empty":
    "A project gives its conversations one working directory, one instruction and a shared memory.",
  "projects.more": "More",
  "projects.edit": "Edit",
  "projects.delete": "Delete",
  "projects.newIn": "New conversation in {name}",
  "projects.options": "Project options for {name}",
  "projects.viewSkills": "View skills",

  "palette.placeholder": "Search conversations, or type a command…",
  "palette.empty": "Nothing matches that.",
  "palette.actions": "Actions",
  "palette.new": "New conversation",
  "palette.find": "Find in conversation",
  "palette.toggleSidebar": "Show or hide conversations",
  "palette.settings": "Settings",
  "palette.theme": "Switch between light and dark",
  "palette.language": "Switch language",
  "palette.conversations": "Conversations",
  "palette.untitled": "Untitled",

  "empty.title": "What should we work on?",
  "empty.lead":
    "Describe the outcome you want. It delegates to sub-agents when the work is worth splitting up. Enter while it runs queues a follow-up; Steer injects into the current turn.",
  "empty.research.title": "Research something broad",
  "empty.research.hint":
    "Several sub-agents look into different angles at once.",
  "empty.research.text":
    "Research a topic from several angles and give me one merged brief with sources.",
  "empty.files.title": "Work through files",
  "empty.files.hint":
    "Attach files with the paperclip or drop them on the box. Paste or drop a screenshot to send it as visual input.",
  "empty.files.text":
    "Go through the files in the workspace and summarise what each one contains.",
  "empty.split.title": "Split a big task",
  "empty.split.hint": "Say the goal; it decides how many workers it needs.",
  "empty.split.text":
    "Break this goal into parallel pieces, work them in parallel, then combine the results.",

  "banner.unconfigured":
    "No model endpoint is configured yet, so nothing can run.",
  "banner.configure": "Configure",
  "banner.dismiss": "Dismiss",

  "composer.placeholder":
    "Describe what you want done. It will delegate as needed.",
  "composer.placeholderRunning": "Working… Enter queues, ⌘Enter steers",
  "composer.placeholderGoal": "Standing objective for this conversation",
  "composer.attach": "Attach files",
  "composer.attachHint": "Attach files to the workspace",
  "composer.thinking": "Thinking level",
  "composer.thinkingDefault": "Default thinking",
  "composer.thinkingLevel": "{level} thinking",
  "reason.low": "Low thinking",
  "reason.medium": "Medium thinking",
  "reason.high": "High thinking",
  "composer.fullAccess": "Full access",
  "composer.fullAccessHint":
    "Agents can read and write files in this conversation's workspace and run commands.",
  "composer.stop": "Stop",
  "composer.send": "Send",
  "composer.sendHint": "Send (Enter)",
  "composer.model": "Model",
  "composer.drop": "Drop files to attach",
  "composer.adding": "Adding files…",
  "composer.removeNamed": "Remove {name}",

  "model.search": "Search models",
  "model.emptyNone": "No models yet. Edit providers to add some.",
  "model.emptyFilter": "No matching model.",
  "model.refresh": "Refresh models",
  "model.edit": "Edit providers",
  "model.fallback": "Model",

  "queue.count": "{n} Queued",
  "queue.clear": "Clear queue",
  "queue.steer": "Steer",
  "queue.steerNamed": "Steer: {text}",
  "queue.removeNamed": "Remove queued message: {text}",

  "goal.pursuing": "Pursuing",
  "goal.done": "Done",
  "goal.paused": "Paused",
  "goal.blocked": "Blocked",
  "goal.clear": "Clear goal",
  "goal.start": "Start goal",
  "goal.edit": "Edit goal",

  "slash.commands": "Commands",
  "slash.goal": "Set a standing objective to pursue until done",
  "slash.compact": "Compact this chat's context",
  "slash.full": "{pct}% full",

  "quote.chipOne": "1 annotation",
  "quote.chipMany": "{n} annotations",
  "quote.selected": "Selected text",
  "quote.dialog": "Quoted text",
  "quote.edit": "Edit selected text {n}",
  "quote.remove": "Remove selected text {n}",

  "selection.menu": "Selection",
  "selection.add": "Add to chat",

  "find.label": "Find in conversation",
  "find.close": "Close find",
  "find.prev": "Previous match",
  "find.next": "Next match",
  "find.none": "No results",
  "find.count": "{current} / {total} results",

  "time.today": "Today",
  "time.yesterday": "Yesterday",
  "time.week": "This week",
  "time.month": "This month",
  "time.earlier": "Earlier",

  "panel.resize": "Resize the side panel",
  "panel.agents": "Agents",
  "panel.files": "Files",
  "panel.trace": "Trace",
  "panel.memory": "Memory",
  "panel.memoryNew": "new",
  "panel.back": "Back to agents",
  "panel.prompt": "View system prompt",
  "panel.promptTitle": "System prompt",
  "panel.promptHint": "What this sub-agent was given as its instruction.",
  "panel.noAgents":
    "No sub-agents yet. The manager starts them when a task is worth splitting up.",
  "panel.active": "Active ({n})",
  "panel.done": "Done ({n})",
  "panel.noTrace": "Nothing has run in this conversation yet.",
  "panel.turn": "Turn",
  "panel.copy": "Copy",
  "panel.copied": "Copied",
  "panel.thinking": "{level} thinking",
  "panel.traceLog": "Full log ({n})",
  "usage.contextPct": "{pct}% context used",
  "usage.tokensUsed": "{used} tokens used",
  "usage.tokensOf": "{used} / {limit} tokens",
  "usage.tokens": "{used} tokens",
  "usage.turnBilled": "This turn billed {line}",
  "usage.conversation": "Conversation {tokens} across {n} {calls}",
  "usage.call": "call",
  "usage.calls": "calls",
  "usage.in": "{n} in",
  "usage.out": "{n} out",
  "usage.cached": "{n} cached",
  "usage.thinking": "{n} thinking",
  "usage.setWindow": "Set a context window in Settings for a percentage",
  "usage.unknownFill": "{used} tokens used",

  "files.upload": "Upload",
  "files.refresh": "Refresh",
  "files.reveal": "Show the workspace in the file manager",
  "files.filter": "Filter files",
  "files.filterPlaceholder": "Filter files...",
  "files.tree": "Workspace files",
  "files.empty":
    "Nothing here yet. Upload files for the agents to work on, or wait for them to produce some.",
  "files.noMatch": "No matching files.",
  "files.download": "Download",
  "files.delete": "Delete",
  "files.yours": "yours",

  "memory.noProject":
    "This conversation is not in a project, so it has nothing to remember between conversations.",
  "memory.reviewNow": "Review this conversation now",
  "memory.reload": "Reload memory",
  "memory.reviewing": "Reading the last finished turn…",
  "memory.off":
    "Memory is switched off for this project. What is already stored stays here and is not used.",
  "memory.notes": "Notes",
  "memory.notesHint":
    "Carried into every conversation in this project. One note per paragraph.",
  "memory.notesLabel": "Project notes",
  "memory.conflict":
    "These notes were updated while you were editing. Save keeps yours; Reload takes the new ones.",
  "memory.storedNow": "Stored now: {text}",
  "memory.saveNotes": "Save notes",
  "memory.reloadNotes": "Reload",
  "memory.revert": "Revert",
  "memory.skills": "Skills",
  "memory.noSkills":
    "No procedures recorded yet. They appear here when a conversation produces one worth following again.",
  "memory.showInFinder": "Show in Finder",
  "memory.deleteSkill": "Delete the skill {name}",
  "memory.delete": "Delete",
  "memory.loading": "Loading…",

  "transcript.queuedSteering": "Queued steering",
  "transcript.jumpLatest": "Jump to latest",
  "transcript.thinking": "Thinking",
  "transcript.thought": "Thought",
  "transcript.continue": "Continue",
  "transcript.stop": "Stop",
  "transcript.continueAria": "Continue for more tool rounds",
  "transcript.stopAria": "Stop at the tool-round limit",
  "transcript.limitAsk":
    "Reached the manager tool-round limit ({limit}). Continue for another {extendBy} rounds?",
  "transcript.limitContinued":
    "Continuing for another {extendBy} tool rounds.",
  "transcript.limitStopped": "Stopped after {limit} tool rounds.",
  "transcript.workingFor": "Working for {duration}",
  "transcript.workingAgents":
    "Working for {duration} · {n} sub-agent{s} running",
  "transcript.workedFor": "Worked for",
  "transcript.stoppedAfter": "Stopped after",
  "transcript.copy": "Copy",
  "transcript.copied": "Copied",
  "transcript.copyMessage": "Copy message",
  "transcript.editMessage": "Edit message",
  "transcript.cancelEdit": "Cancel",
  "transcript.resend": "Send",
  "transcript.steer": "steer",
  "transcript.agentEmpty": "This agent has not produced anything yet.",
  "transcript.waitingFor": "Waiting for {n} sub-agent{s}",
  "transcript.workingEllipsis": "working…",
  "transcript.started": "Started {role}",
  "nav.jump": "Jump to a message",

  "tool.running": "running…",
  "tool.noOutput": "(no output)",
  "tool.emptyFile": "(empty file)",

  "status.running": "running",
  "status.done": "done",
  "status.failed": "failed",
  "status.cancelled": "stopped",

  "notice.goalSet": "Standing objective set.",
  "notice.goalCleared": "Standing objective cleared.",
  "notice.goalCompleted": "Standing objective completed.",
  "notice.goalContinued": "Continuing the standing objective.",
  "notice.goalCapped":
    "Stopped auto-continuing: the standing objective is still open.",
  "notice.goalBlocked":
    "Standing objective blocked: progress needs you or an external change.",
  "notice.goalEdited": "Standing objective updated.",
  "notice.goalResumed": "Resuming the standing objective.",
  "notice.compacted":
    "Earlier turns were folded into a briefing. The transcript is unchanged.",
  "notice.reviewQuiet": "Review finished — nothing new to keep.",
  "notice.reviewDone": "Review finished.",
  "notice.reviewFailed": "Memory review failed: {err}",
  "notice.memoryUpdated": "Memory updated.",

  "settings.title": "Settings",
  "settings.back": "Back to app",
  "settings.search": "Search settings",
  "settings.searchPlaceholder": "Search settings...",
  "settings.noMatch": "No matching settings.",
  "settings.storedIn": "Stored in {path}/config.yaml",
  "settings.storedFallback": "the data directory",

  "settings.nav.general": "General",
  "settings.nav.models": "Models",
  "settings.nav.swarm": "Swarm",
  "settings.nav.tools": "Tools",
  "settings.nav.memory": "Memory",

  "settings.general.title": "General",
  "settings.general.desc":
    "How the window looks, and where this install keeps its files.",
  "settings.general.appearance": "Appearance",
  "settings.general.appearanceHint": "Match the system, or pin light or dark.",
  "settings.general.themeSystem": "Match the system",
  "settings.general.themeLight": "Light",
  "settings.general.themeDark": "Dark",
  "settings.general.language": "Language",
  "settings.general.languageHint":
    "Chrome only. Agents still answer in the language you are using.",
  "settings.general.langSystem": "Match the system",
  "settings.general.langEn": "English",
  "settings.general.langZh": "中文",
  "settings.general.logs": "Logs",
  "settings.general.logLevel": "Log level",
  "settings.general.logHint":
    "Written next to the database in the data directory.",
  "settings.general.install": "This install",
  "settings.general.dataDir": "Data directory",
  "settings.general.version": "Version {version} · {mode} mode",
  "settings.general.mock": " · offline scripted provider",

  "settings.models.title": "Models",
  "settings.models.desc":
    "Each provider is one endpoint. Discover the models it serves, pick a default, and switch per conversation in the composer.",
  "settings.models.providers": "Providers",
  "settings.models.providersDesc":
    "Each endpoint is a row. Open one to edit the URL, key, and default model.",
  "settings.models.add": "Add a provider",
  "settings.models.provider": "Provider",
  "settings.models.providerPlaceholder": "Name this provider",
  "settings.models.noModel": "No model yet",
  "settings.models.default": "default",
  "settings.models.makeDefault": "Make default",
  "settings.models.remove": "Remove {name}",
  "settings.models.details": "{name} details",
  "settings.models.baseUrl": "Base URL",
  "settings.models.apiKey": "API key",
  "settings.models.apiKeySet": "A key is stored. Type to replace it.",
  "settings.models.apiKeyHint": "Leave empty for endpoints that need no key.",
  "settings.models.defaultModel": "Default model",
  "settings.models.defaultModelHint":
    "{n} model{s} on this endpoint. Switch per conversation in the composer.",
  "settings.models.defaultModelEmpty":
    "Discover to list every model this endpoint serves. You only pick the default here.",
  "settings.models.discover": "Discover models",
  "settings.models.pickDefault": "Pick a default",
  "settings.models.typeModel": "discover, or type a model name",
  "settings.models.window": "Context window (tokens)",
  "settings.models.windowHint":
    "How many tokens a model on this endpoint can take, until a name has its own window.",
  "settings.models.windowPlaceholder": "from the endpoint, or leave blank",
  "settings.models.windows": "Context windows",
  "settings.models.windowsHint":
    "Each name on this endpoint has its own limit. Discover fills a value when the listing includes one.",
  "settings.models.windowFor": "{name} context window",
  "settings.models.windowFallback": "Fallback for other names",
  "settings.models.windowFallbackHint":
    "Used only when a name has no window of its own. Do not put one model's limit here.",
  "settings.models.timeout": "Request timeout (seconds)",
  "settings.models.timeoutHint":
    "How long to wait for the next byte from the model. A call that is still streaming is not cut off; a silent endpoint is.",

  "settings.aux.title": "Auxiliary models",
  "settings.aux.desc":
    "These jobs use the conversation's model unless you pin one.",
  "settings.aux.titleGen": "Title generation",
  "settings.aux.compact": "Compact summary",
  "settings.aux.badge": "conversations",
  "settings.aux.auto": "Automatic · this conversation's model",
  "settings.aux.automatic": "Automatic",
  "settings.aux.titleAria": "Title generation model",
  "settings.aux.compactAria": "Compact summary model",

  "settings.swarm.title": "Swarm",
  "settings.swarm.desc":
    "How many workers run at once, and when the manager stops waiting.",
  "settings.swarm.conversations": "Conversations",
  "settings.swarm.autoTitle": "Name conversations automatically",
  "settings.swarm.autoTitleHint":
    "After the first reply, a short title replaces the raw opening line in the sidebar. Off keeps the first message. A name you type is never overwritten. Pin a different model under Models.",
  "settings.swarm.subagents": "Sub-agents",
  "settings.swarm.maxConcurrent": "Sub-agents at once",
  "settings.swarm.maxConcurrentHint":
    "More means faster fan-out and more tokens burned in parallel.",
  "settings.swarm.agentTimeout": "Sub-agent timeout (seconds)",
  "settings.swarm.agentTimeoutHint":
    "How long one sub-agent may keep working before it is stopped.",
  "settings.swarm.maxTurns": "Sub-agent tool rounds",
  "settings.swarm.maxTurnsHint":
    "How many times a sub-agent may think and call a tool before it is stopped.",
  "settings.swarm.manager": "Manager",
  "settings.swarm.managerRounds": "Manager tool rounds",
  "settings.swarm.managerRoundsHint":
    "Spawning and waiting for sub-agents spends the manager's rounds too. Reaching the limit pauses the turn and asks before adding another slice of this size.",
  "settings.swarm.pulse": "Progress pulse (seconds)",
  "settings.swarm.pulseHint":
    "How often a running turn reports in while nothing is streaming.",
  "settings.swarm.coalesce": "Stream coalesce (ms)",
  "settings.swarm.coalesceHint":
    "How long streamed tokens wait to be sent as one event. Lower is snappier; higher is cheaper to render.",
  "settings.swarm.context": "Context",
  "settings.swarm.budget": "Context budget (characters)",
  "settings.swarm.budgetHint":
    "How full /compact treats the replay as. Token windows from the model take precedence when they exist.",
  "settings.swarm.keep": "Messages to keep when compacting",
  "settings.swarm.keepHint":
    "Recent user and assistant messages that stay verbatim. Everything older becomes the briefing.",
  "settings.swarm.goalTurns": "Goal auto-continue turns",
  "settings.swarm.goalTurnsHint":
    "How many consecutive turns the runtime may start to pursue an open /goal without another human message.",

  "settings.tools.title": "Tools",
  "settings.tools.desc":
    "What agents may call. A tool added in a later release keeps its own default.",
  "settings.tools.proxy": "Proxy",
  "settings.tools.proxyDesc": "For tools that reach the network.",
  "settings.tools.http": "HTTP",
  "settings.tools.https": "HTTPS",
  "settings.tools.noProxy": "Skip the proxy for",
  "settings.tools.noProxyHint": "Comma-separated hosts.",

  "settings.memory.title": "Memory",
  "settings.memory.desc":
    "What a project may keep, and how expensive that is per turn.",
  "settings.memory.when": "When it runs",
  "settings.memory.enabled": "Remember anything at all",
  "settings.memory.enabledHint":
    "Off means no project carries notes or skills, whatever its own switch says.",
  "settings.memory.autoReview": "Review a conversation when it finishes",
  "settings.memory.autoReviewHint":
    "A finished turn is read back so durable facts and reusable procedures are kept. Off means memory only changes when an agent or you write to it.",
  "settings.memory.after": "After a review",
  "settings.memory.afterHint":
    "The review still runs and still writes. This only governs the line that appears after the answer.",
  "settings.memory.notifyOn": "One line naming what changed",
  "settings.memory.notifyVerbose": "The line, plus a preview of the text",
  "settings.memory.notifyOff": "Nothing in the transcript",
  "settings.memory.budget": "Budget",
  "settings.memory.charLimit": "Notes budget (characters)",
  "settings.memory.charLimitHint":
    "Every note is in the prompt of every turn in the project, so this is a per-turn cost. Once it is full, an agent must replace a note to add one.",
  "settings.memory.reviewRounds": "Review tool rounds",
  "settings.memory.reviewRoundsHint":
    "How many times the review may think and write before it is stopped.",
  "settings.memory.skillsIndex": "Skills listed in the prompt",
  "settings.memory.skillsIndexHint":
    "Only names and one-line descriptions are listed; an agent opens the one it needs.",

  "project.edit": "Edit project",
  "project.new": "New project",
  "project.desc":
    "Conversations in a project share a working directory, an instruction, and what earlier conversations learned.",
  "project.name": "Name",
  "project.instruction": "Instruction",
  "project.instructionHint":
    "Added to the system prompt of every conversation in this project.",
  "project.workdir": "Working directory",
  "project.workdirPlaceholder": "Leave empty and zwai manages one",
  "project.workdirHint":
    "An absolute path that already exists. The agents read and write it directly, with your permissions.",
  "project.memory": "Memory",
  "project.memoryOn":
    "After each conversation, keep what is worth carrying forward: notes and reusable procedures.",
  "project.memoryOff":
    "Memory is switched off for this install; turn it on in Settings first.",
  "project.storedIn": "Stored in {path}",
  "project.cancel": "Cancel",
  "project.save": "Save",
  "project.create": "Create project",
  "project.deleteTitle": "Delete {name}?",
  "project.deleteDesc":
    "Its conversations and everything it remembered are deleted with it.",
  "project.deleteKeepFiles": " The files in its working directory are left alone.",
  "project.deleteConfirm": "Delete project",

  "confirm.cancel": "Cancel",
  "thread.deleteTitle": "Delete {name}?",
  "thread.deleteDesc":
    "The transcript and this conversation's workspace are removed.",
  "thread.deleteConfirm": "Delete conversation",
  "provider.deleteTitle": "Remove {name}?",
  "provider.deleteDesc":
    "Conversations keep their history. The next turn needs another endpoint.",
  "provider.deleteConfirm": "Remove provider",
  "file.deleteTitle": "Delete {name}?",
  "file.deleteDesc": "This file is removed from the workspace.",
  "file.deleteConfirm": "Delete file",
  "skill.deleteTitle": "Delete {name}?",
  "skill.deleteDesc":
    "Later conversations in this project will not start with this procedure.",
  "skill.deleteConfirm": "Delete skill",
  "queue.deleteTitle": "Remove this queued message?",
  "queue.deleteDesc": "It will not be sent.",
  "queue.deleteConfirm": "Remove",
  "queue.clearTitle": "Clear the queue?",
  "queue.clearDesc": "{n} waiting messages will not be sent.",
  "queue.clearConfirm": "Clear queue",
} as const

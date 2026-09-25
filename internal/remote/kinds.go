package remote

// PushKinds is the desktop SSE KINDS list. A kind missing here is stored
// on the PC and invisible on the phone until someone adds it to both.
var PushKinds = []string{
	"user_message",
	"agent_message",
	"reasoning",
	"reasoning_delta",
	"delta",
	"turn",
	"spawned",
	"finished",
	"tool_call",
	"tool_result",
	"tool_delta",
	"tool_call_delta",
	"steer",
	"steer_retracted",
	"steer_preempted",
	"cleanup",
	"progress",
	"memory_review",
	"max_iterations",
	"max_iterations_continued",
	"model_retry",
	"title",
	"session_memory",
	"done",
	"error",
	"resumed",
	"goal",
	"goal_complete",
	"goal_continued",
	"goal_capped",
	"goal_idle",
	"goal_blocked",
	"goal_edited",
	"goal_resumed",
	"goal_session",
	"plan",
	"plan_updated",
	"plan_implemented",
	"plan_cancelled",
	"compacted",
	"usage",
	"rewound",
	"schedule",
	"schedule_fired",
	"schedule_skipped",
	"schedule_report",
	"schedule_cancelled",
	"message",
}

var pushKindSet = map[string]bool{}

func init() {
	for _, k := range PushKinds {
		pushKindSet[k] = true
	}
}

func shouldPush(kind string) bool {
	return pushKindSet[kind]
}

/** Worker failure details and recovery settings. */
export const agentFailureEn = {
  "agentFailure.viewReason": "View failure reason",
  "agentFailure.title": "Failure reason",
  "agentFailure.parentStopped": "Stopped because this turn failed.",
  "agentFailure.unrecorded": "No failure reason was recorded for this agent.",
  "agentFailure.timeout": "Overall runtime limit reached ({duration}).",
  "agentFailure.maxTurns": "Sub-agent tool-round limit reached.",
  "agentFailure.providerIdle": "The model service stopped sending data for {duration}.",
  "agentFailure.modelSettings": "Model timeout settings",
  "agentFailure.providerHint": "In Models, check the stream idle timeout of the provider used by this task. Adjust it if needed, then start the task again.",
  "agentFailure.timeoutSettings": "Sub-agent timeout",
  "agentFailure.limitHint": "If this task needs a higher limit, adjust it for future runs. Changing the limit does not resume a stopped agent.",
} as const

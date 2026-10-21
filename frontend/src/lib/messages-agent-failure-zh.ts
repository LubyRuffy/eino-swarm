/** Worker failure details and recovery settings. */
export const agentFailureZh = {
  "agentFailure.viewReason": "查看失败原因",
  "agentFailure.title": "失败原因",
  "agentFailure.parentStopped": "因本轮失败而停止。",
  "agentFailure.unrecorded": "未记录这个子代理的失败原因。",
  "agentFailure.timeout": "达到整体运行时限（{duration}）。",
  "agentFailure.maxTurns": "达到子代理工具回合上限。",
  "agentFailure.providerIdle": "模型服务连续 {duration} 未发送数据。",
  "agentFailure.modelSettings": "模型超时设置",
  "agentFailure.providerHint": "在模型设置中检查这个任务所用服务的流式空闲超时，按需调整后重新启动任务。",
  "agentFailure.timeoutSettings": "子代理超时设置",
  "agentFailure.limitHint": "如果任务需要更高上限，可调整后续运行的配置。修改上限不会自动恢复已停止的子代理。",
} as const

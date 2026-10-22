export const generationEn = {
  "generation.interrupted": "Generation interrupted: {reason}.",
  "generation.reason.engine_restart": "engine restart",
  "generation.reason.process_exit": "the previous process exited; its cause was not recorded",
  "generation.reason.output_limit": "model output budget exhausted",
  "generation.reason.model_error": "model request failed",
  "generation.reason.timeout": "generation timed out",
  "generation.reason.cancelled": "generation was cancelled",
  "generation.reason.unknown": "interruption reason was not recorded",
  "generation.recovery.pending": "Recovery pending.",
  "generation.recovery.recovered": "Execution resumed; the interrupted output remains incomplete.",
  "generation.recovery.none": "Automatic recovery is not scheduled.",
}

export const generationZh: Record<keyof typeof generationEn, string> = {
  "generation.interrupted": "本次生成已中断：{reason}。",
  "generation.reason.engine_restart": "引擎重启",
  "generation.reason.process_exit": "上次进程已退出，具体原因未记录",
  "generation.reason.output_limit": "模型输出额度耗尽",
  "generation.reason.model_error": "模型请求失败",
  "generation.reason.timeout": "生成超时",
  "generation.reason.cancelled": "生成被取消",
  "generation.reason.unknown": "中断原因未记录",
  "generation.recovery.pending": "等待恢复。",
  "generation.recovery.recovered": "已恢复执行；原生成内容仍未完成。",
  "generation.recovery.none": "未安排自动恢复。",
}

/** Public turn-error sentence, also present in stored events from older builds. */
export function firstByteTimeoutDuration(text: string): string | undefined {
  return /^the model sent no first byte within ((?:\d+(?:\.\d+)?(?:ns|[µμ]s|ms|s|m|h))+)$/.exec(text)?.[1]
}

export function firstByteTimeoutSettingsKey(providerId: string): string {
  return providerId ? `first_byte_timeout_seconds:${providerId}` : ""
}

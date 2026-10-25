export const voiceSampleRate = 16_000
export const voiceFrameDurationMs = 60
export const voiceFrameSamples = 960

export function supportsVoiceCodecs(): boolean {
  return 'AudioEncoder' in window && 'AudioDecoder' in window
}

export function voiceSocketURL(path: string): string {
  const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${scheme}//${window.location.host}${path}`
}

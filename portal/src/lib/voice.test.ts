import { afterEach, describe, expect, it } from 'vitest'
import {
  supportsVoiceCodecs,
  voiceFrameDurationMs,
  voiceFrameSamples,
  voiceSampleRate,
  voiceSocketURL,
} from './voice'

function defineGlobal(name: string, value: unknown) {
  Object.defineProperty(window, name, { configurable: true, writable: true, value })
}

afterEach(() => {
  // @ts-expect-error the test defines these on purpose.
  delete window.AudioEncoder
  // @ts-expect-error the test defines these on purpose.
  delete window.AudioDecoder
})

describe('voice constants', () => {
  it('match the Opus framing the gateway expects', () => {
    expect(voiceSampleRate).toBe(16_000)
    expect(voiceFrameDurationMs).toBe(60)
    expect(voiceFrameSamples).toBe(960)
  })
})

describe('supportsVoiceCodecs', () => {
  it('requires both the encoder and the decoder', () => {
    expect(supportsVoiceCodecs()).toBe(false)
    defineGlobal('AudioEncoder', class {})
    expect(supportsVoiceCodecs()).toBe(false)
    defineGlobal('AudioDecoder', class {})
    expect(supportsVoiceCodecs()).toBe(true)
  })
})

describe('voiceSocketURL', () => {
  it('builds a ws URL on the current host', () => {
    const url = voiceSocketURL('/voice/dev_1/events')
    expect(url).toBe(`ws://${window.location.host}/voice/dev_1/events`)
  })

  it('upgrades to wss on https', () => {
    const location = window.location
    const fake = { ...location, protocol: 'https:', host: 'oa.example.com' }
    Object.defineProperty(window, 'location', { configurable: true, value: fake })
    try {
      expect(voiceSocketURL('/voice/x/audio')).toBe('wss://oa.example.com/voice/x/audio')
    } finally {
      Object.defineProperty(window, 'location', { configurable: true, value: location })
    }
  })
})

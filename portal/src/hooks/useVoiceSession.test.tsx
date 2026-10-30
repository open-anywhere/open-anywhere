import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useVoiceSession } from './useVoiceSession'
import { fail, mockApi, ok } from '../test/http'

// WebCodecs and the audio graph are not implemented in jsdom. These stand-ins
// record what the hook does with them without producing any sound.

class FakeWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  static instances: FakeWebSocket[] = []
  url: string
  readyState = FakeWebSocket.OPEN
  binaryType = 'blob'
  sent: unknown[] = []
  onmessage: ((event: MessageEvent) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }
  send(data: unknown) {
    this.sent.push(data)
  }
  close() {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.()
  }
}

class FakeEventSource {
  static instances: FakeEventSource[] = []
  url: string
  closed = false
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }
  close() {
    this.closed = true
  }
  emit(payload: unknown) {
    this.onmessage?.({ data: JSON.stringify(payload) } as MessageEvent)
  }
  emitRaw(data: string) {
    this.onmessage?.({ data } as MessageEvent)
  }
}

class FakeAudioContext {
  static instances: FakeAudioContext[] = []
  sampleRate: number
  currentTime = 0
  destination = {}
  closed = false
  gainNodes: { gain: { value: number }; connect: ReturnType<typeof vi.fn> }[] = []
  sources: { start: ReturnType<typeof vi.fn>; stop: ReturnType<typeof vi.fn> }[] = []
  constructor(options?: { sampleRate?: number }) {
    this.sampleRate = options?.sampleRate ?? 48_000
    FakeAudioContext.instances.push(this)
  }
  async resume() {}
  async close() {
    this.closed = true
  }
  createGain() {
    const node = { gain: { value: 1 }, connect: vi.fn() }
    this.gainNodes.push(node)
    return node
  }
  createBuffer(_channels: number, frames: number, rate: number) {
    return { duration: frames / rate, copyToChannel: vi.fn() }
  }
  createBufferSource() {
    const node = { buffer: null, connect: vi.fn(), start: vi.fn(), stop: vi.fn(), onended: null }
    this.sources.push(node)
    return node
  }
  createMediaStreamSource() {
    return { connect: vi.fn() }
  }
  createScriptProcessor() {
    return { connect: vi.fn(), disconnect: vi.fn(), onaudioprocess: null }
  }
}

class FakeAudioEncoder {
  static instances: FakeAudioEncoder[] = []
  state = 'unconfigured'
  init: { output?: (chunk: unknown) => void; error?: (error: Error) => void }
  constructor(init: FakeAudioEncoder['init']) {
    this.init = init
    FakeAudioEncoder.instances.push(this)
  }
  configure() {
    this.state = 'configured'
  }
  encode() {}
  close() {
    this.state = 'closed'
  }
}

class FakeAudioDecoder {
  static instances: FakeAudioDecoder[] = []
  state = 'unconfigured'
  constructor(public init: { output?: (chunk: unknown) => void; error?: () => void }) {
    FakeAudioDecoder.instances.push(this)
  }
  configure() {
    this.state = 'configured'
  }
  decode() {}
  close() {
    this.state = 'closed'
  }
}

const mediaTracks = [{ stop: vi.fn() }]
const getUserMedia = vi.fn(async () => ({ getTracks: () => mediaTracks }))

function installVoiceGlobals({ codecs = true }: { codecs?: boolean } = {}) {
  FakeWebSocket.instances = []
  FakeEventSource.instances = []
  FakeAudioContext.instances = []
  FakeAudioEncoder.instances = []
  FakeAudioDecoder.instances = []
  mediaTracks.forEach((track) => track.stop.mockClear())
  getUserMedia.mockClear()

  vi.stubGlobal('WebSocket', FakeWebSocket)
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.stubGlobal('AudioContext', FakeAudioContext)
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia },
  })
  Object.defineProperty(window, 'AudioData', {
    configurable: true,
    writable: true,
    value: class {},
  })
  Object.defineProperty(window, 'EncodedAudioChunk', {
    configurable: true,
    writable: true,
    value: class {},
  })
  if (codecs) {
    Object.defineProperty(window, 'AudioEncoder', {
      configurable: true,
      writable: true,
      value: FakeAudioEncoder,
    })
    Object.defineProperty(window, 'AudioDecoder', {
      configurable: true,
      writable: true,
      value: FakeAudioDecoder,
    })
  } else {
    // @ts-expect-error removed on purpose to simulate an unsupported browser.
    delete window.AudioEncoder
    // @ts-expect-error removed on purpose to simulate an unsupported browser.
    delete window.AudioDecoder
  }
}

function statusRoute(overrides: Record<string, unknown> = {}) {
  return {
    'GET /voice/dev_1/status': ok({
      state: 'listening',
      connected: true,
      volume: 40,
      mac: 'AA:BB',
      registered: true,
      ...overrides,
    }),
  }
}

beforeEach(() => {
  installVoiceGlobals()
})

afterEach(() => {
  vi.useRealTimers()
  // @ts-expect-error restored by the next test.
  delete window.AudioEncoder
  // @ts-expect-error restored by the next test.
  delete window.AudioDecoder
})

describe('useVoiceSession status', () => {
  it('loads the voice status and adopts the server volume', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await waitFor(() => expect(result.current.status?.state).toBe('listening'))
    expect(result.current.volume).toBe(40)
    expect(result.current.statusError).toBeUndefined()
    expect(result.current.supported).toBe(true)
  })

  it('reports a status failure without breaking the page', async () => {
    mockApi({ 'GET /voice/dev_1/status': fail(503, 'UNAVAILABLE', '智能体不在线') })
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await waitFor(() => expect(result.current.statusError).toBe('智能体不在线'))
  })

  it('polls the status every three seconds', async () => {
    vi.useFakeTimers()
    const http = mockApi(statusRoute())
    renderHook(() => useVoiceSession('dev_1'))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(http.callsFor('GET', '/voice/dev_1/status')).toHaveLength(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000)
    })
    expect(http.callsFor('GET', '/voice/dev_1/status')).toHaveLength(2)
  })
})

describe('useVoiceSession events', () => {
  it('collects transcripts by role', async () => {
    const http = mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))
    await waitFor(() => expect(http.calls.length).toBeGreaterThan(0))

    const source = FakeEventSource.instances[0]
    act(() => source.emit({ type: 'stt', text: '来一场抢答' }))
    act(() => source.emit({ type: 'tts', text: '没问题' }))
    act(() => source.emit({ type: 'mcp', text: 'start_app' }))

    expect(result.current.entries.map((entry) => [entry.role, entry.text])).toEqual([
      ['user', '来一场抢答'],
      ['agent', '没问题'],
      ['tool', '正在执行：start_app'],
    ])
  })

  it('drops repeated connection states but keeps different ones', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    const source = FakeEventSource.instances[0]
    act(() => source.emit({ type: 'state', state: 'listening' }))
    act(() => source.emit({ type: 'state', state: 'listening' }))
    act(() => source.emit({ type: 'state', state: 'thinking' }))

    expect(result.current.entries.map((entry) => entry.text)).toEqual(['listening', 'thinking'])
  })

  it('applies a volume event from the platform', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    act(() => FakeEventSource.instances[0].emit({ type: 'volume', text: '25' }))
    expect(result.current.volume).toBe(25)

    act(() => FakeEventSource.instances[0].emit({ type: 'volume', text: 'nope' }))
    expect(result.current.volume).toBe(25)
  })

  it('ignores audio frames, malformed JSON and empty events', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    const source = FakeEventSource.instances[0]
    act(() => source.emit({ type: 'audio', text: 'bin' }))
    act(() => source.emit({ type: 'stt' }))
    act(() => source.emitRaw('{not json'))
    expect(result.current.entries).toEqual([])
  })

  it('reports a broken event stream', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    act(() => FakeEventSource.instances[0].onerror?.())
    expect(result.current.streamError).toBe('连接中断，正在重连…')

    act(() => FakeEventSource.instances[0].onopen?.())
    expect(result.current.streamError).toBeUndefined()
  })

  it('closes the event stream on unmount', async () => {
    mockApi(statusRoute())
    const { unmount } = renderHook(() => useVoiceSession('dev_1'))
    const source = FakeEventSource.instances[0]

    unmount()
    expect(source.closed).toBe(true)
  })
})

describe('useVoiceSession start', () => {
  it('refuses to start without WebCodecs', async () => {
    installVoiceGlobals({ codecs: false })
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.start()
    })

    expect(result.current.error).toContain('当前浏览器不支持 WebCodecs')
    expect(result.current.micActive).toBe(false)
    expect(result.current.supported).toBe(false)
  })

  it('opens the playback and microphone sockets', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.start()
    })

    expect(FakeWebSocket.instances.map((socket) => socket.url)).toEqual([
      `ws://${window.location.host}/voice/dev_1/playback`,
      `ws://${window.location.host}/voice/dev_1/audio?codec=opus`,
    ])
    expect(result.current.playbackActive).toBe(true)
    expect(result.current.micActive).toBe(true)
    expect(result.current.starting).toBe(false)
    expect(FakeAudioEncoder.instances).toHaveLength(1)
    expect(FakeAudioEncoder.instances[0].state).toBe('configured')
  })

  it('creates the decoder when the server announces the sample rate', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.start()
    })
    const playback = FakeWebSocket.instances[0]
    expect(FakeAudioDecoder.instances).toHaveLength(0)

    act(() => playback.onmessage?.({ data: '{"sample_rate":24000}' } as MessageEvent))
    expect(FakeAudioDecoder.instances).toHaveLength(1)
    expect(FakeAudioDecoder.instances[0].state).toBe('configured')

    // A second announcement replaces the decoder instead of stacking them.
    act(() => playback.onmessage?.({ data: '{}' } as MessageEvent))
    expect(FakeAudioDecoder.instances).toHaveLength(2)
    expect(FakeAudioDecoder.instances[0].state).toBe('closed')
  })

  it('only decodes binary frames once the decoder is ready', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.start()
    })
    const playback = FakeWebSocket.instances[0]
    const decode = vi.spyOn(FakeAudioDecoder.prototype, 'decode')

    act(() => playback.onmessage?.({ data: new ArrayBuffer(4) } as MessageEvent))
    expect(decode).not.toHaveBeenCalled()

    act(() => playback.onmessage?.({ data: '{}' } as MessageEvent))
    act(() => playback.onmessage?.({ data: new ArrayBuffer(4) } as MessageEvent))
    expect(decode).toHaveBeenCalledTimes(1)
  })

  it('flushes queued audio when the platform aborts a reply', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.start()
    })
    const context = FakeAudioContext.instances[0]
    const source = { buffer: null, connect: vi.fn(), start: vi.fn(), stop: vi.fn(), onended: null }
    context.createBufferSource = () => source

    // Produce one queued buffer through the decoder output.
    act(() => FakeWebSocket.instances[0].onmessage?.({ data: '{}' } as MessageEvent))
    const decoder = FakeAudioDecoder.instances[0]
    act(() =>
      decoder.init.output?.({
        numberOfFrames: 480,
        sampleRate: 48_000,
        copyTo: vi.fn(),
        close: vi.fn(),
      }),
    )
    expect(source.start).toHaveBeenCalled()
    expect(source.stop).not.toHaveBeenCalled()

    act(() => FakeEventSource.instances[0].emit({ type: 'tts', text: '', is_aborted: true }))
    expect(source.stop).toHaveBeenCalled()
    expect(result.current.entries).toEqual([])
  })

  it('turns the microphone off when its socket closes', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.start()
    })
    act(() => FakeWebSocket.instances[1].onclose?.())

    expect(result.current.micActive).toBe(false)
  })

  it('reports socket errors without dropping the session', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.start()
    })
    act(() => FakeWebSocket.instances[0].onerror?.())
    expect(result.current.error).toBe('语音播放连接失败，请稍后重试。')

    act(() => FakeWebSocket.instances[0].onclose?.())
    expect(result.current.playbackActive).toBe(false)

    await act(async () => {
      await result.current.start()
    })
    // The restarted session opens fresh playback and microphone sockets.
    expect(FakeWebSocket.instances).toHaveLength(4)
    act(() => FakeWebSocket.instances[3].onerror?.())
    expect(result.current.error).toBe('麦克风上传连接失败，请稍后重试。')
  })

  it('stops everything when the microphone is denied', async () => {
    getUserMedia.mockRejectedValueOnce(new Error('Permission denied'))
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.start()
    })

    expect(result.current.error).toBe('Permission denied')
    expect(result.current.micActive).toBe(false)
    expect(result.current.playbackActive).toBe(false)
    expect(FakeAudioContext.instances.every((context) => context.closed)).toBe(true)
  })

  it('toggles the microphone and playback independently', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.start()
    })
    expect(result.current.micActive).toBe(true)

    await act(async () => {
      await result.current.toggleMicrophone()
    })
    expect(result.current.micActive).toBe(false)
    expect(mediaTracks[0].stop).toHaveBeenCalled()

    await act(async () => {
      await result.current.togglePlayback()
    })
    expect(result.current.playbackActive).toBe(false)

    await act(async () => {
      await result.current.togglePlayback()
    })
    expect(result.current.playbackActive).toBe(true)
  })

  it('keeps the microphone off when the device fails to start', async () => {
    getUserMedia.mockRejectedValueOnce(new Error('denied'))
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.toggleMicrophone()
    })

    expect(result.current.error).toBe('denied')
    expect(result.current.micActive).toBe(false)
  })

  it('reports a playback failure without a decoder', async () => {
    installVoiceGlobals({ codecs: true })
    // @ts-expect-error simulate a browser with an encoder but no decoder.
    delete window.AudioDecoder
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.togglePlayback()
    })

    expect(result.current.error).toContain('不支持 WebCodecs 音频解码')
  })
})

describe('useVoiceSession controls', () => {
  it('pauses, resumes and aborts through the gateway', async () => {
    const http = mockApi({
      ...statusRoute(),
      'POST /voice/dev_1/pause': ok({}),
      'POST /voice/dev_1/resume': ok({}),
      'POST /voice/dev_1/abort': ok({}),
    })
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.pause()
    })
    await act(async () => {
      await result.current.resume()
    })
    await act(async () => {
      await result.current.abort()
    })

    expect(http.calls.map((call) => `${call.method} ${call.path}`)).toEqual(
      expect.arrayContaining([
        'POST /voice/dev_1/pause',
        'POST /voice/dev_1/resume',
        'POST /voice/dev_1/abort',
      ]),
    )
    expect(result.current.error).toBeUndefined()
  })

  it('reports each control failure', async () => {
    mockApi({
      ...statusRoute(),
      'POST /voice/dev_1/pause': fail(409, 'BUSY', '正在回答'),
      'POST /voice/dev_1/resume': fail(409, 'BUSY', '无法继续'),
      'POST /voice/dev_1/abort': fail(409, 'BUSY', '无法打断'),
    })
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    await act(async () => {
      await result.current.pause()
    })
    expect(result.current.error).toBe('正在回答')

    await act(async () => {
      await result.current.resume()
    })
    expect(result.current.error).toBe('无法继续')

    await act(async () => {
      await result.current.abort()
    })
    expect(result.current.error).toBe('无法打断')
  })

  it('exposes volume changes and clears transcripts and errors', async () => {
    mockApi(statusRoute())
    const { result } = renderHook(() => useVoiceSession('dev_1'))

    act(() => result.current.setVolume(150))
    expect(result.current.volume).toBe(100)
    act(() => result.current.setVolume(-10))
    expect(result.current.volume).toBe(0)
    act(() => result.current.setVolume(55.6))
    expect(result.current.volume).toBe(56)

    act(() => FakeEventSource.instances[0].emit({ type: 'stt', text: '你好' }))
    expect(result.current.entries).toHaveLength(1)
    act(() => result.current.clearEntries())
    expect(result.current.entries).toEqual([])

    act(() => result.current.stop())
    expect(result.current.error).toBeUndefined()
  })
})

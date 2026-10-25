import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, voice, type VoiceEvent, type VoiceStatus } from '../api'
import {
  supportsVoiceCodecs,
  voiceFrameDurationMs,
  voiceFrameSamples,
  voiceSampleRate,
  voiceSocketURL,
} from '../lib/voice'

export interface TranscriptEntry {
  id: number
  type: string
  role: 'user' | 'agent' | 'system' | 'tool' | 'error'
  text: string
  at: number
}

function roleFor(type: string): TranscriptEntry['role'] {
  switch (type) {
    case 'stt':
      return 'user'
    case 'tts':
      return 'agent'
    case 'mcp':
      return 'tool'
    case 'error':
      return 'error'
    default:
      return 'system'
  }
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error && error.message) return error.message
  return fallback
}

export function useVoiceSession(deviceId: string) {
  const [status, setStatus] = useState<VoiceStatus>()
  const [statusError, setStatusError] = useState<string>()
  const [streamError, setStreamError] = useState<string>()
  const [error, setError] = useState<string>()
  const [entries, setEntries] = useState<TranscriptEntry[]>([])
  const [micActive, setMicActive] = useState(false)
  const [playbackActive, setPlaybackActive] = useState(false)
  const [starting, setStarting] = useState(false)
  const [volume, setVolumeState] = useState(100)

  const mountedRef = useRef(true)
  const startingRef = useRef(false)
  const entryIdRef = useRef(0)
  const volumeRef = useRef(100)
  const volumeTouchedRef = useRef(false)

  const micStreamRef = useRef<MediaStream | null>(null)
  const micContextRef = useRef<AudioContext | null>(null)
  const micProcessorRef = useRef<ScriptProcessorNode | null>(null)
  const micSocketRef = useRef<WebSocket | null>(null)
  const micEncoderRef = useRef<AudioEncoder | null>(null)
  const micTimestampRef = useRef(0)
  const micBufferRef = useRef(new Float32Array(0))

  const playbackContextRef = useRef<AudioContext | null>(null)
  const playbackGainRef = useRef<GainNode | null>(null)
  const playbackSocketRef = useRef<WebSocket | null>(null)
  const playbackDecoderRef = useRef<AudioDecoder | null>(null)
  const playbackAtRef = useRef(0)
  const playbackTimestampRef = useRef(0)
  const playingRef = useRef<AudioBufferSourceNode[]>([])

  const applyServerVolume = useCallback((value: number) => {
    const clamped = Math.max(0, Math.min(100, Math.round(value)))
    volumeTouchedRef.current = false
    volumeRef.current = clamped
    setVolumeState(clamped)
    if (playbackGainRef.current) {
      playbackGainRef.current.gain.value = clamped / 100
    }
  }, [])

  const changeVolume = useCallback((value: number) => {
    const clamped = Math.max(0, Math.min(100, Math.round(value)))
    volumeTouchedRef.current = true
    volumeRef.current = clamped
    setVolumeState(clamped)
    if (playbackGainRef.current) {
      playbackGainRef.current.gain.value = clamped / 100
    }
  }, [])

  const flushPlayback = useCallback(() => {
    for (const source of playingRef.current) {
      try {
        source.stop()
      } catch {
        // The source may already have ended.
      }
    }
    playingRef.current = []
    playbackAtRef.current = 0
  }, [])

  const stopPlayback = useCallback(() => {
    flushPlayback()
    const socket = playbackSocketRef.current
    playbackSocketRef.current = null
    if (socket) {
      socket.onmessage = null
      socket.onclose = null
      socket.onerror = null
      try {
        socket.close()
      } catch {
        // Already closed.
      }
    }
    const decoder = playbackDecoderRef.current
    playbackDecoderRef.current = null
    try {
      decoder?.close()
    } catch {
      // Already closed.
    }
    const context = playbackContextRef.current
    playbackContextRef.current = null
    playbackGainRef.current = null
    playbackAtRef.current = 0
    if (context) {
      void context.close().catch(() => undefined)
    }
    if (mountedRef.current) setPlaybackActive(false)
  }, [flushPlayback])

  const startPlayback = useCallback(async () => {
    if (playbackSocketRef.current) return
    if (!('AudioDecoder' in window)) {
      throw new Error('当前浏览器不支持 WebCodecs 音频解码，请使用 Chrome 或 Edge。')
    }

    const context = new AudioContext()
    await context.resume()
    const gain = context.createGain()
    gain.gain.value = volumeRef.current / 100
    gain.connect(context.destination)
    playbackContextRef.current = context
    playbackGainRef.current = gain
    playbackAtRef.current = 0
    playbackTimestampRef.current = 0

    const socket = new WebSocket(voiceSocketURL(voice.playbackURL(deviceId)))
    socket.binaryType = 'arraybuffer'
    playbackSocketRef.current = socket
    let decoder: AudioDecoder | undefined

    socket.onmessage = (event) => {
      if (typeof event.data === 'string') {
        try {
          const info = JSON.parse(event.data) as { sample_rate?: number }
          const sampleRate = info.sample_rate || voiceSampleRate
          try {
            decoder?.close()
          } catch {
            // Already closed.
          }
          decoder = new AudioDecoder({
            output: (chunk) => {
              try {
                const buffer = context.createBuffer(1, chunk.numberOfFrames, chunk.sampleRate)
                const channel = new Float32Array(chunk.numberOfFrames)
                chunk.copyTo(channel, { planeIndex: 0, format: 'f32-planar' })
                chunk.close()
                buffer.copyToChannel(channel, 0)
                const source = context.createBufferSource()
                source.buffer = buffer
                source.connect(gain)
                const at = Math.max(context.currentTime + 0.05, playbackAtRef.current)
                source.start(at)
                playbackAtRef.current = at + buffer.duration
                playingRef.current.push(source)
                source.onended = () => {
                  playingRef.current = playingRef.current.filter((item) => item !== source)
                }
              } catch {
                if (mountedRef.current) {
                  setError('声音播放遇到问题，请稍后重试。')
                }
              }
            },
            error: () => {
              if (mountedRef.current) {
                setError('声音播放遇到问题，请稍后重试。')
              }
            },
          })
          decoder.configure({
            codec: 'opus',
            sampleRate,
            numberOfChannels: 1,
          })
          playbackDecoderRef.current = decoder
        } catch {
          if (mountedRef.current) {
            setError('声音播放遇到问题，请稍后重试。')
          }
        }
        return
      }
      if (event.data instanceof ArrayBuffer && decoder?.state === 'configured') {
        decoder.decode(
          new EncodedAudioChunk({
            type: 'key',
            timestamp: playbackTimestampRef.current,
            data: event.data,
          }),
        )
        playbackTimestampRef.current += voiceFrameDurationMs * 1000
      }
    }

    socket.onclose = () => {
      if (playbackSocketRef.current === socket) {
        playbackSocketRef.current = null
        playbackDecoderRef.current = null
        try {
          decoder?.close()
        } catch {
          // Already closed.
        }
        const currentContext = playbackContextRef.current
        playbackContextRef.current = null
        playbackGainRef.current = null
        flushPlayback()
        if (currentContext) {
          void currentContext.close().catch(() => undefined)
        }
        if (mountedRef.current) setPlaybackActive(false)
      }
    }
    socket.onerror = () => {
      if (mountedRef.current) setError('语音播放连接失败，请稍后重试。')
    }

    setPlaybackActive(true)
  }, [deviceId, flushPlayback])

  const stopMicrophone = useCallback(() => {
    const stream = micStreamRef.current
    micStreamRef.current = null
    stream?.getTracks().forEach((track) => track.stop())

    const processor = micProcessorRef.current
    micProcessorRef.current = null
    try {
      processor?.disconnect()
    } catch {
      // Already disconnected.
    }

    const socket = micSocketRef.current
    micSocketRef.current = null
    if (socket) {
      socket.onclose = null
      socket.onerror = null
      try {
        socket.close()
      } catch {
        // Already closed.
      }
    }

    const encoder = micEncoderRef.current
    micEncoderRef.current = null
    try {
      encoder?.close()
    } catch {
      // Already closed.
    }

    const context = micContextRef.current
    micContextRef.current = null
    if (context) {
      void context.close().catch(() => undefined)
    }

    micBufferRef.current = new Float32Array(0)
    if (mountedRef.current) setMicActive(false)
  }, [])

  const startMicrophone = useCallback(async () => {
    if (!('AudioEncoder' in window)) {
      throw new Error('当前浏览器不支持 WebCodecs 音频编码，请使用 Chrome 或 Edge。')
    }

    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true },
    })
    micStreamRef.current = stream

    const context = new AudioContext({ sampleRate: voiceSampleRate })
    await context.resume()
    micContextRef.current = context

    const socket = new WebSocket(voiceSocketURL(voice.audioURL(deviceId)))
    socket.binaryType = 'arraybuffer'
    micSocketRef.current = socket
    socket.onclose = () => {
      if (micStreamRef.current && mountedRef.current) {
        stopMicrophone()
      }
    }
    socket.onerror = () => {
      if (mountedRef.current) setError('麦克风上传连接失败，请稍后重试。')
    }

    const encoder = new AudioEncoder({
      output: (chunk) => {
        if (socket.readyState !== WebSocket.OPEN) return
        const bytes = new Uint8Array(chunk.byteLength)
        chunk.copyTo(bytes)
        socket.send(bytes)
      },
      error: (encodeError) => {
        if (mountedRef.current) setError(`音频编码失败：${encodeError.message}`)
      },
    })
    encoder.configure({
      codec: 'opus',
      sampleRate: voiceSampleRate,
      numberOfChannels: 1,
      bitrate: 24_000,
      opus: { frameDuration: voiceFrameDurationMs * 1000 },
    })
    micEncoderRef.current = encoder

    const source = context.createMediaStreamSource(stream)
    const processor = context.createScriptProcessor(4096, 1, 1)
    micProcessorRef.current = processor
    micTimestampRef.current = 0
    micBufferRef.current = new Float32Array(0)

    processor.onaudioprocess = (event) => {
      const input = event.inputBuffer.getChannelData(0)
      const merged = new Float32Array(micBufferRef.current.length + input.length)
      merged.set(micBufferRef.current)
      merged.set(input, micBufferRef.current.length)
      let offset = 0
      while (merged.length - offset >= voiceFrameSamples && encoder.state === 'configured') {
        const frame = merged.slice(offset, offset + voiceFrameSamples)
        offset += voiceFrameSamples
        encoder.encode(
          new AudioData({
            format: 'f32-planar',
            sampleRate: voiceSampleRate,
            numberOfFrames: voiceFrameSamples,
            numberOfChannels: 1,
            timestamp: micTimestampRef.current,
            data: frame,
          }),
        )
        micTimestampRef.current += voiceFrameDurationMs * 1000
      }
      micBufferRef.current = merged.slice(offset)
    }

    source.connect(processor)
    processor.connect(context.destination)
    setMicActive(true)
  }, [deviceId, stopMicrophone])

  const refreshStatus = useCallback(async () => {
    try {
      const next = await voice.status(deviceId)
      if (!mountedRef.current) return
      setStatus(next)
      setStatusError(undefined)
      if (typeof next.volume === 'number' && !volumeTouchedRef.current) {
        applyServerVolume(next.volume)
      }
    } catch (statusFailure) {
      if (!mountedRef.current) return
      setStatusError(errorMessage(statusFailure, '暂时无法连接智能体'))
    }
  }, [deviceId, applyServerVolume])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      stopMicrophone()
      stopPlayback()
    }
  }, [stopMicrophone, stopPlayback])

  useEffect(() => {
    void refreshStatus()
    const timer = window.setInterval(() => {
      void refreshStatus()
    }, 3_000)
    return () => window.clearInterval(timer)
  }, [refreshStatus])

  useEffect(() => {
    const source = new EventSource(voice.eventsURL(deviceId))
    source.onopen = () => setStreamError(undefined)
    source.onerror = () => setStreamError('连接中断，正在重连…')
    source.onmessage = (event) => {
      let payload: VoiceEvent & { error?: string }
      try {
        payload = JSON.parse(event.data) as VoiceEvent & { error?: string }
      } catch {
        return
      }
      if (payload.type === 'audio') return
      if (payload.type === 'volume') {
        const value = Number(payload.text)
        if (Number.isFinite(value)) applyServerVolume(value)
        return
      }
      if (payload.type === 'tts' && payload.is_aborted) {
        flushPlayback()
      }
      const text = payload.text || payload.state || payload.error || ''
      if (!text) return
      const entry: TranscriptEntry = {
        id: ++entryIdRef.current,
        type: payload.type,
        role: roleFor(payload.type),
        text: payload.type === 'mcp' ? `正在执行：${text}` : text,
        at: Date.now(),
      }
      setEntries((current) => {
        const last = current[current.length - 1]
        // 重连会重放当前状态，连续相同的状态事件不必重复展示。
        if (entry.type === 'state' && last?.type === 'state' && last.text === entry.text) {
          return current
        }
        return [...current, entry].slice(-200)
      })
      void refreshStatus()
    }
    return () => source.close()
  }, [deviceId, applyServerVolume, flushPlayback, refreshStatus])

  const start = useCallback(async () => {
    if (startingRef.current) return
    if (!supportsVoiceCodecs()) {
      setError('当前浏览器不支持 WebCodecs 音频，请使用 Chrome 或 Edge，并通过 localhost 或 HTTPS 访问。')
      return
    }
    startingRef.current = true
    setStarting(true)
    setError(undefined)
    try {
      await startPlayback()
      await startMicrophone()
      void refreshStatus()
    } catch (startFailure) {
      stopMicrophone()
      stopPlayback()
      setError(errorMessage(startFailure, '暂时无法开始对话'))
    } finally {
      startingRef.current = false
      if (mountedRef.current) setStarting(false)
    }
  }, [refreshStatus, startMicrophone, startPlayback, stopMicrophone, stopPlayback])

  const stop = useCallback(() => {
    stopMicrophone()
    stopPlayback()
    setError(undefined)
  }, [stopMicrophone, stopPlayback])

  const toggleMicrophone = useCallback(async () => {
    if (micStreamRef.current) {
      stopMicrophone()
      return
    }
    try {
      await startMicrophone()
    } catch (microphoneFailure) {
      setError(errorMessage(microphoneFailure, '麦克风不可用'))
    }
  }, [startMicrophone, stopMicrophone])

  const togglePlayback = useCallback(async () => {
    if (playbackSocketRef.current) {
      stopPlayback()
      return
    }
    try {
      await startPlayback()
    } catch (playbackFailure) {
      setError(errorMessage(playbackFailure, '暂时无法播放声音'))
    }
  }, [startPlayback, stopPlayback])

  const pause = useCallback(async () => {
    try {
      await voice.pause(deviceId)
      await refreshStatus()
    } catch (pauseFailure) {
      setError(errorMessage(pauseFailure, '暂时无法暂停'))
    }
  }, [deviceId, refreshStatus])

  const resume = useCallback(async () => {
    try {
      await voice.resume(deviceId)
      await refreshStatus()
    } catch (resumeFailure) {
      setError(errorMessage(resumeFailure, '暂时无法继续'))
    }
  }, [deviceId, refreshStatus])

  const abort = useCallback(async () => {
    flushPlayback()
    try {
      await voice.abort(deviceId)
      await refreshStatus()
    } catch (abortFailure) {
      setError(errorMessage(abortFailure, '暂时无法打断'))
    }
  }, [deviceId, flushPlayback, refreshStatus])

  return {
    status,
    statusError,
    streamError,
    error,
    entries,
    micActive,
    playbackActive,
    starting,
    volume,
    supported: supportsVoiceCodecs(),
    start,
    stop,
    pause,
    resume,
    abort,
    toggleMicrophone,
    togglePlayback,
    setVolume: changeVolume,
  }
}

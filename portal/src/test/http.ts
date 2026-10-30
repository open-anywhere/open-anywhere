import { vi } from 'vitest'

export interface ApiCall {
  method: string
  path: string
  url: URL
  body: unknown
}

export interface MockResponse {
  status?: number
  /** JSON body; ignored when `text` is set. */
  json?: unknown
  text?: string
}

export type MockValue =
  | MockResponse
  | ((call: ApiCall) => MockResponse | Promise<MockResponse>)

export interface MockApi {
  calls: ApiCall[]
  /** Calls that had no matching route; every one answered 404. */
  unexpected: ApiCall[]
  fetchMock: ReturnType<typeof vi.fn>
  callsFor: (method: string, path: string) => ApiCall[]
}

function parseBody(body: unknown): unknown {
  if (typeof body !== 'string' || body === '') return undefined
  try {
    return JSON.parse(body)
  } catch {
    return body
  }
}

function responseFor(status: number, body: string | undefined): Response {
  return {
    status,
    ok: status >= 200 && status < 300,
    text: async () => body ?? '',
    json: async () => JSON.parse(body ?? 'null'),
  } as unknown as Response
}

/**
 * Replace `window.fetch` with a table of routes keyed by `"METHOD /path"`.
 * A route can also be registered under `"METHOD /path?query"` and `"*"`.
 * Requests without a route answer 404 and are recorded in `unexpected`.
 */
export function mockApi(routes: Record<string, MockValue>): MockApi {
  const calls: ApiCall[] = []
  const unexpected: ApiCall[] = []

  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const raw = typeof input === 'string' ? input : input.toString()
    const url = new URL(raw, window.location.origin)
    const method = (init?.method ?? 'GET').toUpperCase()
    const call: ApiCall = { method, path: url.pathname, url, body: parseBody(init?.body) }
    calls.push(call)

    const route =
      routes[`${method} ${url.pathname}${url.search}`] ??
      routes[`${method} ${url.pathname}`] ??
      routes[`*`]

    if (route === undefined) {
      unexpected.push(call)
      return responseFor(404, JSON.stringify({ error: { code: 'NOT_FOUND', message: `no route for ${method} ${url.pathname}` } }))
    }

    const result = typeof route === 'function' ? await route(call) : route
    const status = result.status ?? 200
    if (status === 204) return responseFor(204, undefined)
    if (result.text !== undefined) return responseFor(status, result.text)
    return responseFor(status, JSON.stringify(result.json ?? null))
  })

  vi.stubGlobal('fetch', fetchMock)

  return {
    calls,
    unexpected,
    fetchMock,
    callsFor: (method, path) =>
      calls.filter((call) => call.method === method && call.path === path),
  }
}

/** A single JSON response for one endpoint. */
export function ok(json: unknown, status = 200): MockResponse {
  return { status, json }
}

/** An error response shaped like the gateway's error envelope. */
export function fail(status: number, code: string, message = '出了点问题'): MockResponse {
  return { status, json: { error: { code, message } } }
}

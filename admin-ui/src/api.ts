const configuredApiBase = (import.meta.env.VITE_API_BASE as string | undefined)?.trim().replace(/\/+$/, '')

export class ApiError extends Error {
  status: number
  payload: unknown

  constructor(message: string, status: number, payload: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.payload = payload
  }
}

function requestUrl(path: string): string {
  const normalizedPath = path.replace(/^\/+/, '')
  return configuredApiBase ? `${configuredApiBase}/api/${normalizedPath}` : `api/${normalizedPath}`
}

async function parseResponse(response: Response): Promise<unknown> {
  if (response.status === 204) return undefined
  const contentType = response.headers.get('content-type') || ''
  if (contentType.includes('application/json')) return response.json()
  return response.text()
}

export async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const response = await fetch(requestUrl(path), { ...init, headers })
  const payload = await parseResponse(response)
  if (!response.ok) {
    const record = payload && typeof payload === 'object' ? payload as Record<string, unknown> : undefined
    const message = String(record?.error || record?.message || payload || `请求失败：${response.status} ${response.statusText}`)
    throw new ApiError(message, response.status, payload)
  }
  return payload as T
}

export async function apiDownload(path: string): Promise<Blob> {
  const response = await fetch(requestUrl(path), { headers: { Accept: 'text/yaml, text/plain, application/json' } })
  if (!response.ok) {
    const payload = await parseResponse(response)
    throw new ApiError(String(payload || `请求失败：${response.status}`), response.status, payload)
  }
  return response.blob()
}

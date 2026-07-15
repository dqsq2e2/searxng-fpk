const apiBase = (import.meta.env.VITE_API_BASE as string | undefined)?.replace(/\/$/, '') || ''

export async function apiRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const normalizedPath = path.replace(/^\/+/, '')
  const response = await fetch(`${apiBase}/api/${normalizedPath}`, {
    ...init,
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      ...init?.headers,
    },
  })

  if (!response.ok) {
    throw new Error(`请求失败：${response.status} ${response.statusText}`)
  }

  return response.json() as Promise<T>
}

export async function apiGet<T>(path: string): Promise<T> {
  const resp = await fetch(path)
  if (!resp.ok) throw new Error(await resp.text())
  return resp.json() as Promise<T>
}

export async function apiPost<T>(path: string): Promise<T> {
  const resp = await fetch(path, { method: 'POST' })
  if (!resp.ok) throw new Error(await resp.text())
  return resp.json() as Promise<T>
}

export async function apiPostJSON<T>(path: string, body: unknown): Promise<T> {
  const resp = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!resp.ok) throw new Error(await resp.text())
  return resp.json() as Promise<T>
}

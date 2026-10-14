export async function api(path, options = {}) {
  const response = await fetch(path, { ...options, cache: 'no-store' })
  const data = await response.json()
  if (!response.ok) throw new Error((data.error || '请求失败，请稍后重试。') + (data.unmapped_targets?.length ? '：' + data.unmapped_targets.join('、') : ''))
  return data
}

export function post(path, body, method = 'POST') {
  return api(path, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
}

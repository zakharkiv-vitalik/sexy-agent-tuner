export function dur(ns: number): string {
  const s = Math.round(ns / 1e9)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}

export function tok(n: number): string {
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'k'
  return String(n)
}

export function bytes(n: number): string {
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MB'
  if (n >= 1 << 10) return (n / (1 << 10)).toFixed(0) + ' KB'
  return `${n} B`
}

export function rel(path: string, project: string): string {
  return project && path.startsWith(project + '/') ? path.slice(project.length + 1) : path
}

export function short(s: string, n: number): string {
  s = s.replace(/<\/?pasted_content[^>]*>/g, ' ').replace(/\s+/g, ' ').trim()
  return s.length > n ? s.slice(0, n - 1) + '…' : s
}

export function date(iso: string): string {
  const d = new Date(iso)
  return isNaN(+d) ? '' : d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

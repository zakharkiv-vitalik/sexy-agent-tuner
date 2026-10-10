import type { analysis } from '../../wailsjs/go/models'

export type Lane = 'prompt' | 'read' | 'search' | 'edit' | 'test' | 'mcp' | 'delegate' | 'other'

// Lanes fold the analysis categories into at most eight identities so each
// keeps a validated color; the lane label, not the color, carries identity.
export const LANES: { id: Lane; label: string }[] = [
  { id: 'prompt', label: 'Prompt' },
  { id: 'read', label: 'Read' },
  { id: 'search', label: 'Search' },
  { id: 'edit', label: 'Edit' },
  { id: 'test', label: 'Test' },
  { id: 'mcp', label: 'MCP' },
  { id: 'delegate', label: 'Subagent/skill' },
  { id: 'other', label: 'Other' },
]

export function laneOf(e: analysis.TimelineEntry): Lane | null {
  if (e.Kind === 'user_prompt') return 'prompt'
  if (e.Kind === 'assistant_text') return null
  switch (e.Category) {
    case 'read': case 'search': case 'edit': case 'test': case 'mcp':
      return e.Category
    case 'subagent': case 'skill':
      return 'delegate'
  }
  return 'other'
}

export const laneColor = (l: Lane) => `var(--lane-${l})`
export const laneLabel = (l: Lane) => LANES.find(x => x.id === l)!.label

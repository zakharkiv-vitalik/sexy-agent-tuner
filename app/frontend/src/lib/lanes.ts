import type { analysis } from '../../wailsjs/go/models'

export type Lane = 'prompt' | 'command' | 'read' | 'search' | 'edit' | 'test' | 'mcp' | 'skill' | 'subagent' | 'other'

// Lanes fold the analysis categories into a handful of identities; the lane
// label, not the color, carries identity, and empty lanes are hidden.
export const LANES: { id: Lane; label: string }[] = [
  { id: 'prompt', label: 'Prompt' },
  { id: 'command', label: 'Command' },
  { id: 'read', label: 'Read' },
  { id: 'search', label: 'Search' },
  { id: 'edit', label: 'Edit' },
  { id: 'test', label: 'Test' },
  { id: 'mcp', label: 'MCP' },
  { id: 'skill', label: 'Skill' },
  { id: 'subagent', label: 'Subagent' },
  { id: 'other', label: 'Other' },
]

export function laneOf(e: analysis.TimelineEntry): Lane | null {
  if (e.Kind === 'user_prompt') return e.Name ? 'command' : 'prompt' // Name is set for slash commands
  if (e.Kind === 'assistant_text') return null
  switch (e.Category) {
    case 'read': case 'search': case 'edit': case 'test': case 'mcp':
      return e.Category
    case 'skill':
      return 'skill'
    case 'subagent':
      return 'subagent'
  }
  return 'other'
}

export const laneColor = (l: Lane) => `var(--lane-${l})`
export const laneLabel = (l: Lane) => LANES.find(x => x.id === l)!.label

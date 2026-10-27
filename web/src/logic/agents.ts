export interface NamedAgent {
  name: string;
}

export interface AgentCapability {
  name: string;
  available: boolean;
}

// pickDefaultAgent keeps the global default authoritative, but falls back to
// opencode or the first usable agent when the configured one is unavailable.
// An empty usable list keeps the configured value so a temporarily empty probe
// never rewrites the user's preference.
export function pickDefaultAgent(configured: string, usable: NamedAgent[]): string {
  const wanted = configured.trim();
  if (wanted && (usable.length === 0 || usable.some((a) => a.name === wanted))) {
    return wanted;
  }
  return usable.find((a) => a.name === 'opencode')?.name || usable[0]?.name || 'opencode';
}

// availableRemoteAgentNames returns the names a remote runner reported.
export function availableRemoteAgentNames(capabilities: AgentCapability[] | undefined): Set<string> {
  return new Set((capabilities ?? []).filter((a) => a.available).map((a) => a.name));
}

// filterRemoteCapable hides configured agents that the remote runner did not
// find. Before the probe finishes (known=false) the list is left untouched.
export function filterRemoteCapable<T extends NamedAgent>(
  agents: T[],
  capabilities: AgentCapability[] | undefined,
  known: boolean,
): T[] {
  if (!known) return agents;
  const names = availableRemoteAgentNames(capabilities);
  return agents.filter((a) => names.has(a.name));
}

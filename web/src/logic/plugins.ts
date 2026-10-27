export interface PluginUI {
  entry: string;
  title: string;
  slots: string[];
}

export interface SlotPlugin {
  id: string;
  enabled: boolean;
  ui?: PluginUI;
}

// pluginUIURL points the trusted same-origin iframe at the plugin's UI entry. The
// backend requires entries to live below ui/, so the prefix is stripped.
export function pluginUIURL(plugin: { id: string; ui?: PluginUI }, slot?: string): string {
  const entry = (plugin.ui?.entry || 'ui/index.html').replace(/^ui\//, '');
  const encoded = entry
    .split('/')
    .filter(Boolean)
    .map((part) => encodeURIComponent(part))
    .join('/');
  const query = slot ? `?slot=${encodeURIComponent(slot)}` : '';
  return `/api/plugins/${encodeURIComponent(plugin.id)}/ui/${encoded}${query}`;
}

// pluginsForSlot returns enabled plugins that declared this host slot.
export function pluginsForSlot<T extends SlotPlugin>(plugins: T[], slot: string): T[] {
  return plugins.filter((p) => p.enabled && !!p.ui && p.ui.slots.includes(slot));
}

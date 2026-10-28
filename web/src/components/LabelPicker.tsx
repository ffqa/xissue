import { useMemo } from 'react';

export const COMMON_LABELS = [
  'feature',
  'bug',
  'enhancement',
  'question',
  'documentation',
  'ui',
  'api',
];

export function splitLabels(raw: string): string[] {
  return raw
    .split(/[,，\n]/)
    .map((label) => label.trim())
    .filter(Boolean);
}

export function toggleLabel(raw: string, label: string): string {
  const labels = splitLabels(raw);
  const exists = labels.some((item) => item.toLowerCase() === label.toLowerCase());
  const next = exists
    ? labels.filter((item) => item.toLowerCase() !== label.toLowerCase())
    : [...labels, label];
  return next.join(', ');
}

export function LabelPicker({
  value,
  onChange,
  title = '标签（逗号分隔）',
}: {
  value: string;
  onChange: (value: string) => void;
  title?: string;
}) {
  const selected = useMemo(() => new Set(splitLabels(value).map((label) => label.toLowerCase())), [value]);
  const labels = splitLabels(value);
  return (
    <label className="field">
      {title}
      <div className="row" style={{ gap: 4, flexWrap: 'wrap', marginBottom: 4 }}>
        {COMMON_LABELS.map((label) => (
          <button
            key={label}
            type="button"
            className={`badge label-chip${selected.has(label) ? ' selected' : ''}`}
            onClick={() => onChange(toggleLabel(value, label))}
          >
            {label}
          </button>
        ))}
      </div>
      <input
        value={value}
        placeholder="bug, frontend, urgent"
        onChange={(e) => onChange(e.target.value)}
      />
      {labels.length > 0 && (
        <span className="row" style={{ gap: 4, flexWrap: 'wrap', marginTop: 4 }}>
          {labels.map((label) => (
            <span className="badge" key={label}>{label}</span>
          ))}
        </span>
      )}
    </label>
  );
}

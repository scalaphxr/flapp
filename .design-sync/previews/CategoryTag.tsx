import { CategoryTag } from 'flapp';

const ALL = ["808", "Kick", "Snare", "Clap", "Hi-Hat", "Open Hat", "Perc", "Vox", "FX", "Loop", "Drum Loop"];

const W = ({ children }: { children: any }) => (
  <div data-theme="warm-dark" style={{ background: 'var(--bg-base)', padding: '24px', display: 'flex', flexWrap: 'wrap', gap: '8px', alignItems: 'center' }}>
    {children}
  </div>
);

export const AllCategories = () => (
  <W>
    {ALL.map(cat => <CategoryTag key={cat} category={cat} />)}
  </W>
);

export const NoDot = () => (
  <W>
    <CategoryTag category="Kick" dot={false} />
    <CategoryTag category="Snare" dot={false} />
    <CategoryTag category="808" dot={false} />
    <CategoryTag category="Hi-Hat" dot={false} />
  </W>
);

export const CustomLabel = () => (
  <W>
    <CategoryTag category="Hi-Hat" label="Hat" />
    <CategoryTag category="Drum Loop" label="Loops" />
    <CategoryTag category="Open Hat" label="OH" />
  </W>
);

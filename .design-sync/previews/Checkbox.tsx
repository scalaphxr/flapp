import { Checkbox } from 'flapp';

const W = ({ children }: { children: any }) => (
  <div data-theme="warm-dark" style={{ background: 'var(--bg-base)', padding: '24px', display: 'flex', flexDirection: 'column', gap: '16px' }}>
    {children}
  </div>
);

export const States = () => (
  <W>
    <Checkbox label="Include sub-folders" checked />
    <Checkbox label="Overwrite duplicates" />
    <Checkbox label="Show file extensions" checked disabled />
    <Checkbox label="Auto-import on startup" disabled />
  </W>
);

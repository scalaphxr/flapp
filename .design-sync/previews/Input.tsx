import { Input, Icons } from 'flapp';

const W = ({ children }: { children: any }) => (
  <div data-theme="warm-dark" style={{ background: 'var(--bg-base)', padding: '24px', display: 'flex', flexDirection: 'column', gap: '12px', width: '280px' }}>
    {children}
  </div>
);

export const Plain = () => (
  <W>
    <Input placeholder="Sample name…" />
  </W>
);

export const WithIcon = () => (
  <W>
    <Input icon={<Icons.Search />} placeholder="Search samples…" />
    <Input icon={<Icons.Folder />} placeholder="Browse path…" />
  </W>
);

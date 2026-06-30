import { Button, Icons } from 'flapp';

const W = ({ children }: { children: any }) => (
  <div data-theme="warm-dark" style={{ background: 'var(--bg-base)', padding: '24px', display: 'flex', alignItems: 'center', gap: '12px', flexWrap: 'wrap' }}>
    {children}
  </div>
);

export const Primary = () => (
  <W>
    <Button variant="primary">Import Samples</Button>
    <Button variant="primary" icon={<Icons.Plus />}>Add Library</Button>
    <Button variant="primary" disabled>Processing…</Button>
  </W>
);

export const Secondary = () => (
  <W>
    <Button variant="secondary">Export</Button>
    <Button variant="secondary" icon={<Icons.Download />}>Download</Button>
    <Button variant="secondary" disabled>Unavailable</Button>
  </W>
);

export const Ghost = () => (
  <W>
    <Button variant="ghost">Cancel</Button>
    <Button variant="ghost" icon={<Icons.X />}>Clear Filter</Button>
  </W>
);

export const Danger = () => (
  <W>
    <Button variant="danger">Delete Sample</Button>
    <Button variant="danger" icon={<Icons.Trash />}>Remove All</Button>
  </W>
);

export const Sizes = () => (
  <W>
    <Button size="sm" variant="primary">Small</Button>
    <Button size="md" variant="primary">Medium</Button>
    <Button size="lg" variant="primary">Large</Button>
  </W>
);

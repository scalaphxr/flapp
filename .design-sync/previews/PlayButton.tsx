import { PlayButton } from 'flapp';

const W = ({ children }: { children: any }) => (
  <div data-theme="warm-dark" style={{ background: 'var(--bg-base)', padding: '24px', display: 'flex', alignItems: 'center', gap: '16px' }}>
    {children}
  </div>
);

export const Idle = () => (
  <W>
    <PlayButton size={28} />
    <PlayButton size={36} />
    <PlayButton size={48} />
  </W>
);

export const Playing = () => (
  <W>
    <PlayButton playing size={28} />
    <PlayButton playing size={36} />
    <PlayButton playing size={48} />
  </W>
);

import { Tabs, Icons } from 'flapp';

const W = ({ children }: { children: any }) => (
  <div data-theme="warm-dark" style={{ background: 'var(--bg-base)', padding: '24px', display: 'flex', flexDirection: 'column', gap: '12px', alignItems: 'flex-start' }}>
    {children}
  </div>
);

export const Navigation = () => (
  <W>
    <Tabs
      value="samples"
      tabs={[
        { key: 'samples', label: 'Samples' },
        { key: 'player', label: 'Player' },
        { key: 'midi', label: 'MIDI' },
        { key: 'analytics', label: 'Analytics' },
      ]}
    />
  </W>
);

export const WithIcons = () => (
  <W>
    <Tabs
      value="player"
      tabs={[
        { key: 'samples', label: 'Library', icon: <Icons.Music /> },
        { key: 'player', label: 'Player', icon: <Icons.Play /> },
        { key: 'midi', label: 'MIDI', icon: <Icons.Midi /> },
        { key: 'settings', label: 'Settings', icon: <Icons.Gear /> },
      ]}
    />
  </W>
);

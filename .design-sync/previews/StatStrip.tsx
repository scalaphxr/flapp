import { StatStrip } from 'flapp';

const W = ({ children }: { children: any }) => (
  <div data-theme="warm-dark" style={{ background: 'var(--bg-base)', padding: '24px', display: 'flex', flexDirection: 'column', gap: '12px', alignItems: 'flex-start' }}>
    {children}
  </div>
);

export const AudioStats = () => (
  <W>
    <StatStrip items={[
      { value: '847', label: 'samples' },
      { value: '2.4 GB', label: 'total', accent: true },
      { value: '41 min', label: 'duration' },
    ]} />
  </W>
);

export const Simple = () => (
  <W>
    <StatStrip items={['128 BPM', '4/4', 'WAV 44.1kHz']} />
  </W>
);

export const WithAccent = () => (
  <W>
    <StatStrip items={[
      { value: '12', label: 'playing', accent: true },
      { value: '3', label: 'queued' },
      'Live',
    ]} />
  </W>
);

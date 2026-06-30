import { ProgressBar } from 'flapp';

const W = ({ children }: { children: any }) => (
  <div data-theme="warm-dark" style={{ background: 'var(--bg-base)', padding: '24px', display: 'flex', flexDirection: 'column', gap: '16px', width: '300px' }}>
    {children}
  </div>
);

export const Values = () => (
  <W>
    <ProgressBar value={0} />
    <ProgressBar value={35} />
    <ProgressBar value={68} />
    <ProgressBar value={100} />
  </W>
);

export const WithCaption = () => (
  <W>
    <ProgressBar value={42} caption="Importing samples…" percent />
    <ProgressBar value={100} caption="Analysis complete" percent />
    <ProgressBar value={17} caption="Scanning folder" percent />
  </W>
);

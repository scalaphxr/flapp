import { Card } from 'flapp';

const W = ({ children }: { children: any }) => (
  <div data-theme="warm-dark" style={{ background: 'var(--bg-base)', padding: '24px', display: 'flex', gap: '16px', flexWrap: 'wrap', alignItems: 'flex-start' }}>
    {children}
  </div>
);

export const Default = () => (
  <W>
    <Card style={{ width: 200 }}>
      <p style={{ margin: 0, color: 'var(--text-body)', fontSize: 'var(--fs-body)', fontWeight: 'var(--fw-medium)' as any }}>
        TR-808 Drum Kit
      </p>
      <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 'var(--fs-sm)' }}>
        64 samples · 240 MB
      </p>
    </Card>
  </W>
);

export const Elevated = () => (
  <W>
    <Card elevated style={{ width: 200 }}>
      <p style={{ margin: 0, color: 'var(--text-body)', fontWeight: 'var(--fw-semibold)' as any }}>
        Lo-Fi Hip Hop Pack
      </p>
      <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 'var(--fs-sm)' }}>
        128 samples · 1.2 GB
      </p>
    </Card>
  </W>
);

export const NoPadding = () => (
  <W>
    <Card padding={0} style={{ width: 220, overflow: 'hidden' }}>
      <div style={{ padding: '10px 16px', background: 'var(--surface-3)', borderBottom: '1px solid var(--border-soft)' }}>
        <span style={{ color: 'var(--text-strong)', fontSize: 'var(--fs-sm)', fontWeight: 'var(--fw-semibold)' as any }}>Library Stats</span>
      </div>
      <div style={{ padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 6 }}>
        <span style={{ color: 'var(--text-body)', fontSize: 'var(--fs-sm)' }}>847 samples</span>
        <span style={{ color: 'var(--text-muted)', fontSize: 'var(--fs-sm)' }}>2.4 GB total</span>
      </div>
    </Card>
  </W>
);

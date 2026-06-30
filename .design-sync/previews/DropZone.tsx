import { DropZone, Icons } from 'flapp';

const W = ({ children }: { children: any }) => (
  <div data-theme="warm-dark" style={{ background: 'var(--bg-base)', padding: '24px' }}>
    {children}
  </div>
);

export const Default = () => (
  <W>
    <DropZone
      title="Drop samples here"
      subtitle="WAV, MP3, FLAC, AIF — up to 4 GB"
      style={{ width: 320 }}
    />
  </W>
);

export const WithIcon = () => (
  <W>
    <DropZone
      title="Import from folder"
      subtitle="Drag a folder to scan all audio files"
      icon={<Icons.Folder />}
      style={{ width: 320 }}
    />
  </W>
);

export const Active = () => (
  <W>
    <DropZone
      title="Release to import"
      subtitle="Drop your samples here"
      active
      style={{ width: 320 }}
    />
  </W>
);

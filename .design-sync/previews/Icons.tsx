import { Icons } from 'flapp';

const W = ({ children }: { children: any }) => (
  <div data-theme="warm-dark" style={{ background: 'var(--bg-base)', padding: '24px', display: 'flex', flexWrap: 'wrap', gap: '20px', alignItems: 'flex-start' }}>
    {children}
  </div>
);

const Icon = ({ name, children }: { name: string; children: any }) => (
  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '6px' }}>
    <div style={{ color: 'var(--text-body)', width: 24, height: 24, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      {children}
    </div>
    <span style={{ color: 'var(--text-faint)', fontSize: '10px', fontFamily: 'var(--font-mono)' }}>{name}</span>
  </div>
);

export const AllIcons = () => (
  <W>
    <Icon name="Wave"><Icons.Wave /></Icon>
    <Icon name="Tool"><Icons.Tool /></Icon>
    <Icon name="Gear"><Icons.Gear /></Icon>
    <Icon name="Search"><Icons.Search /></Icon>
    <Icon name="Folder"><Icons.Folder /></Icon>
    <Icon name="Zip"><Icons.Zip /></Icon>
    <Icon name="Flp"><Icons.Flp /></Icon>
    <Icon name="Audio"><Icons.Audio /></Icon>
    <Icon name="Trash"><Icons.Trash /></Icon>
    <Icon name="Plus"><Icons.Plus /></Icon>
    <Icon name="Save"><Icons.Save /></Icon>
    <Icon name="Info"><Icons.Info /></Icon>
    <Icon name="Stop"><Icons.Stop /></Icon>
    <Icon name="Heart"><Icons.Heart /></Icon>
    <Icon name="Star"><Icons.Star /></Icon>
    <Icon name="Wand"><Icons.Wand /></Icon>
    <Icon name="Box"><Icons.Box /></Icon>
    <Icon name="Chart"><Icons.Chart /></Icon>
    <Icon name="X"><Icons.X /></Icon>
    <Icon name="Check"><Icons.Check /></Icon>
    <Icon name="Music"><Icons.Music /></Icon>
    <Icon name="Play"><Icons.Play /></Icon>
    <Icon name="Pause"><Icons.Pause /></Icon>
    <Icon name="SkipBack"><Icons.SkipBack /></Icon>
    <Icon name="SkipFwd"><Icons.SkipFwd /></Icon>
    <Icon name="Volume"><Icons.Volume /></Icon>
    <Icon name="Midi"><Icons.Midi /></Icon>
    <Icon name="Download"><Icons.Download /></Icon>
    <Icon name="ChevronDown"><Icons.ChevronDown /></Icon>
  </W>
);

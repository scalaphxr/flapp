import React from "react";
import { TopBar } from "@/widgets/TopBar/TopBar";
import { Icons, type TabItem } from "@/shared/ui";
import { useT } from "@/shared/i18n";
import { useJobsStore } from "@/shared/model/jobs";
import { useSettingsStore } from "@/shared/model/settings";
import { SamplesPage } from "@/pages/samples/SamplesPage";
import { SettingsPage } from "@/pages/settings/SettingsPage";

const AnalyticsPage = React.lazy(() =>
  import("@/pages/analytics/AnalyticsPage").then((m) => ({ default: m.AnalyticsPage }))
);
const PlayerPage = React.lazy(() =>
  import("@/pages/player/PlayerPage").then((m) => ({ default: m.PlayerPage }))
);
type TabKey = "sounds" | "analytics" | "player" | "settings";

export default function App() {
  const t = useT();
  const [active, setActive] = React.useState<TabKey>("sounds");
  const connect = useJobsStore((s) => s.connect);
  const loadSettings = useSettingsStore((s) => s.load);

  React.useEffect(() => {
    connect();
    loadSettings();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const tabs: TabItem[] = [
    { key: "sounds",   label: t.nav.sounds,   icon: <Icons.Wave /> },
    { key: "player",   label: t.nav.player,   icon: <Icons.Music /> },
    { key: "settings", label: t.nav.settings, icon: <Icons.Gear /> },
  ];

  return (
    <div className="app-shell">
      <TopBar tabs={tabs} active={active} onChange={(k) => setActive(k as TabKey)} />
      <main className="app-main">
        {active === "sounds" ? <SamplesPage /> : null}
        {active === "analytics" ? (
          <React.Suspense fallback={<div className="page-desc">{t.common.loading}</div>}>
            <AnalyticsPage />
          </React.Suspense>
        ) : null}
        {active === "player" ? (
          <React.Suspense fallback={<div className="page-desc">{t.common.loading}</div>}>
            <PlayerPage />
          </React.Suspense>
        ) : null}
        {active === "settings" ? <SettingsPage /> : null}
      </main>
    </div>
  );
}

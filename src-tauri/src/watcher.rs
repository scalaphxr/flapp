// Live-слежение за папками плеера: notify-вотчер + пересканирование с
// дебаунсом. Команда player_watch_folders задаёт набор папок; рабочий поток
// держит снапшот аудиофайлов и на любое FS-событие перечитывает деревья,
// отправляя diff событием "player-fs-change" {added, removed}.
//
// Пересканирование вместо разбора отдельных событий делает логику устойчивой к
// особенностям Windows (rename приходит как Remove+Create, копирование сыплет
// Modify): каждое срабатывание сверяется с реальным состоянием диска. Файл,
// который ещё докопируется, не отдаётся наверх, пока его размер не перестанет
// меняться между двумя проверками, — иначе анализатор прочитал бы обрезок.

use std::collections::{BTreeSet, HashMap};
use std::path::Path;
use std::sync::mpsc::{channel, Receiver, RecvTimeoutError, Sender};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use notify::{RecommendedWatcher, RecursiveMode, Watcher};
use serde::Serialize;
use tauri::{AppHandle, Emitter, State};

const DEBOUNCE: Duration = Duration::from_millis(900);
const TICK: Duration = Duration::from_millis(250);

#[derive(Clone, Serialize)]
pub struct FsChange {
    pub added: Vec<String>,
    pub removed: Vec<String>,
}

enum Msg {
    /// Новый полный набор папок для слежения (пустой = выключить).
    SetDirs(Vec<String>),
    /// Что-то изменилось в одной из папок — детали не важны, будет rescan.
    Touched,
}

/// Канал к рабочему потоку; сам поток стартует лениво при первом вызове команды.
#[derive(Default)]
pub struct WatchState(Mutex<Option<Sender<Msg>>>);

#[tauri::command]
pub fn player_watch_folders(
    dirs: Vec<String>,
    app: AppHandle,
    state: State<WatchState>,
) -> Result<(), String> {
    let mut guard = state.0.lock().unwrap();
    if guard.is_none() {
        let (tx, rx) = channel::<Msg>();
        let tx_events = tx.clone();
        std::thread::spawn(move || worker(app, rx, tx_events));
        *guard = Some(tx);
    }
    guard
        .as_ref()
        .unwrap()
        .send(Msg::SetDirs(dirs))
        .map_err(|e| e.to_string())
}

fn worker(app: AppHandle, rx: Receiver<Msg>, tx: Sender<Msg>) {
    // Держит подписки живыми; сам объект после создания не читается.
    let mut _watcher: Option<RecommendedWatcher> = None;
    let mut dirs: Vec<String> = Vec::new();
    let mut snapshot: BTreeSet<String> = BTreeSet::new();
    // Кандидаты на добавление: путь → размер при последней проверке.
    let mut pending: HashMap<String, u64> = HashMap::new();
    let mut dirty_at: Option<Instant> = None;

    loop {
        match rx.recv_timeout(TICK) {
            Ok(Msg::SetDirs(new_dirs)) => {
                dirs = new_dirs;
                _watcher = None; // дропает все старые подписки
                if !dirs.is_empty() {
                    let txc = tx.clone();
                    if let Ok(mut w) = notify::recommended_watcher(move |_res| {
                        let _ = txc.send(Msg::Touched);
                    }) {
                        for d in &dirs {
                            // Папка могла исчезнуть (флешка) — просто пропускаем.
                            let _ = w.watch(Path::new(d), RecursiveMode::Recursive);
                        }
                        _watcher = Some(w);
                    }
                }
                // Стартовый снапшот без события: начальную синхронизацию
                // (включая изменения, накопленные пока приложение было
                // закрыто) фронтенд делает сам через player_scan_folder.
                snapshot = scan_all(&dirs);
                pending.clear();
                dirty_at = None;
            }
            Ok(Msg::Touched) => dirty_at = Some(Instant::now()),
            Err(RecvTimeoutError::Timeout) => {}
            Err(RecvTimeoutError::Disconnected) => return,
        }

        let due = matches!(dirty_at, Some(t) if t.elapsed() >= DEBOUNCE);
        if !due {
            continue;
        }
        dirty_at = None;

        let current = scan_all(&dirs);
        let removed: Vec<String> = snapshot.difference(&current).cloned().collect();
        let mut added: Vec<String> = Vec::new();
        for path in current.difference(&snapshot) {
            let size = std::fs::metadata(path).map(|m| m.len()).unwrap_or(0);
            match pending.get(path) {
                Some(&prev) if prev == size => {
                    pending.remove(path);
                    added.push(path.clone());
                }
                _ => {
                    // Размер ещё не подтверждён — перепроверим следующим циклом.
                    pending.insert(path.clone(), size);
                    dirty_at = Some(Instant::now());
                }
            }
        }
        pending.retain(|p, _| current.contains(p));

        if added.is_empty() && removed.is_empty() {
            continue;
        }
        for p in &added {
            snapshot.insert(p.clone());
        }
        for p in &removed {
            snapshot.remove(p);
        }
        let _ = app.emit("player-fs-change", FsChange { added, removed });
    }
}

fn scan_all(dirs: &[String]) -> BTreeSet<String> {
    let mut set = BTreeSet::new();
    for d in dirs {
        let mut files = Vec::new();
        crate::analyzer::scan_dir_recursive(d, &mut files);
        set.extend(files);
    }
    set
}

// The Tauri shell is intentionally thin: Tauri's core is Rust, so the Go
// backend cannot *be* the Tauri backend. Instead this shell launches the
// compiled Go binary as a sidecar process, reads the "PORT=NNNN" line it prints
// on startup, and hands that port to the web front-end (both as an invokable
// command and as a one-shot "backend-ready" event). All application logic lives
// in Go and the React UI; this file only manages the child process lifecycle.

mod analyzer;

use std::sync::Mutex;

use tauri::{Emitter, Manager, RunEvent, State};
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

/// Shared state holding the backend port (once known) and the child handle so
/// it can be terminated when the app exits.
#[derive(Default)]
struct Backend {
    port: Mutex<Option<u16>>,
    child: Mutex<Option<CommandChild>>,
}

/// Returns the backend port to the front-end. The UI polls this (or listens for
/// the "backend-ready" event) before making its first API call.
#[tauri::command]
fn get_backend_port(state: State<Backend>) -> Option<u16> {
    *state.port.lock().unwrap()
}

/// Читает аудиофайл с диска и возвращает сырые байты как бинарный ответ.
/// На JS-стороне invoke<ArrayBuffer> получает данные без JSON-сериализации.
#[tauri::command]
async fn player_read_audio(path: String) -> Result<tauri::ipc::Response, String> {
    let bytes = tauri::async_runtime::spawn_blocking(move || {
        std::fs::read(&path).map_err(|e| e.to_string())
    })
    .await
    .map_err(|e| e.to_string())??;
    Ok(tauri::ipc::Response::new(bytes))
}

pub fn run() {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .manage(Backend::default())
        .manage(analyzer::AnalyzerCache::new())
        .invoke_handler(tauri::generate_handler![
            get_backend_port,
            player_read_audio,
            analyzer::player_analyze_file,
            analyzer::player_analyze_batch,
            analyzer::player_decode_to_wav,
            analyzer::player_scan_folder,
            analyzer::player_get_dates,
        ])
        .setup(|app| {
            let handle = app.handle().clone();

            // Launch the Go backend as a sidecar. The binary is bundled per
            // platform as binaries/flapp-core-<target-triple>.
            let sidecar = app.shell().sidecar("flapp-core")?;
            let (mut rx, child) = sidecar.spawn()?;

            {
                let state = handle.state::<Backend>();
                *state.child.lock().unwrap() = Some(child);
            }

            // Read the sidecar's stdout asynchronously, looking for the port.
            tauri::async_runtime::spawn(async move {
                while let Some(event) = rx.recv().await {
                    match event {
                        CommandEvent::Stdout(bytes) => {
                            let line = String::from_utf8_lossy(&bytes);
                            if let Some(port) = parse_port(&line) {
                                let state = handle.state::<Backend>();
                                *state.port.lock().unwrap() = Some(port);
                                let _ = handle.emit("backend-ready", port);
                            }
                        }
                        CommandEvent::Stderr(bytes) => {
                            // Surface backend logs in the dev console.
                            eprintln!("[flapp-core] {}", String::from_utf8_lossy(&bytes));
                        }
                        CommandEvent::Terminated(payload) => {
                            eprintln!("[flapp-core] exited: {:?}", payload.code);
                        }
                        _ => {}
                    }
                }
            });

            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building the application");

    // Make sure the Go sidecar is stopped when the window/app closes.
    app.run(|app_handle, event| {
        if let RunEvent::ExitRequested { .. } = event {
            let state = app_handle.state::<Backend>();
            let child = state.child.lock().unwrap().take();
            if let Some(child) = child {
                let _ = child.kill();
            }
        }
    });
}

/// Extracts a port from a "PORT=NNNN" line, ignoring anything else.
fn parse_port(line: &str) -> Option<u16> {
    line.trim()
        .strip_prefix("PORT=")
        .and_then(|rest| rest.trim().parse::<u16>().ok())
}

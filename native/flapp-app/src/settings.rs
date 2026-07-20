use serde::{Deserialize, Serialize};
use std::path::PathBuf;

/// Minimal settings mirror. Only the fields the native app needs so far; the
/// existing settings.json (written by the Go/Tauri app) carries many more keys,
/// which serde ignores on load. SQLite and the full settings surface arrive in
/// later sub-projects.
#[derive(Serialize, Deserialize, Clone, Default)]
pub struct Settings {
    #[serde(default)]
    pub language: String,
    #[serde(default)]
    pub theme: String,
}

/// Matches the Go/Tauri app: os.UserConfigDir()/flapp/settings.json.
pub fn config_path() -> PathBuf {
    let base = dirs::config_dir().unwrap_or_else(|| PathBuf::from("."));
    base.join("flapp").join("settings.json")
}

/// Load settings; a missing, malformed, or partial file never panics — it falls
/// back to defaults.
pub fn load() -> Settings {
    match std::fs::read_to_string(config_path()) {
        Ok(text) => serde_json::from_str(&text).unwrap_or_default(),
        Err(_) => Settings::default(),
    }
}

/// Persist settings (used by later sub-projects; the Foundation does not write).
pub fn save(s: &Settings) -> std::io::Result<()> {
    let path = config_path();
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir)?;
    }
    let text = serde_json::to_string_pretty(s).unwrap();
    std::fs::write(path, text)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trips_and_tolerates_unknown_fields() {
        // Existing settings.json has many more keys; unknown keys must not break load.
        let json = r#"{"language":"ru","theme":"warm-dark","ytNickname":"x","workers":4}"#;
        let s: Settings = serde_json::from_str(json).unwrap();
        assert_eq!(s.language, "ru");
        assert_eq!(s.theme, "warm-dark");
        let back = serde_json::to_string(&s).unwrap();
        let s2: Settings = serde_json::from_str(&back).unwrap();
        assert_eq!(s2.language, "ru");
    }
}

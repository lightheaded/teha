// SPDX-License-Identifier: Apache-2.0

//! Where the shell keeps what the person enters once.
//!
//! The server address and the shortcut go into a JSON file in the application
//! configuration directory. The device token goes into the platform credential
//! store, which is the keychain on macOS. A token never reaches that JSON
//! file, a log line or the repository.

use std::fs;
use std::io::Write;
use std::path::PathBuf;

use keyring::Entry;
use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager};
use url::Url;

/// The global shortcut that the shell registers when nobody chose one.
/// `desktop/README.md` documents it.
pub const DEFAULT_SHORTCUT: &str = "CmdOrCtrl+Shift+A";

/// The service name of the credential store entry. The account name is the
/// server address, so two servers can each hold a token.
const KEYCHAIN_SERVICE: &str = "io.github.lightheaded.teha.desktop";

/// The name of the settings file inside the application configuration
/// directory.
const FILE: &str = "settings.json";

/// A quick add line is one line. The limit stops an outside URL from pushing a
/// whole document into the field.
const MAX_LINE: usize = 500;

/// The two settings that the shell needs to start.
#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Settings {
    /// The origin of the server, for example `https://teha.example`.
    pub server: String,
    /// The global shortcut, in the accelerator form that Tauri parses.
    pub shortcut: String,
}

/// The answer to the settings window. It reports that a token exists and never
/// sends the token itself back to a page.
#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct View {
    pub server: String,
    pub shortcut: String,
    pub has_token: bool,
    pub default_shortcut: String,
}

fn file_path(app: &AppHandle) -> Result<PathBuf, String> {
    let dir = app
        .path()
        .app_config_dir()
        .map_err(|err| format!("the configuration directory is not known: {err}"))?;
    Ok(dir.join(FILE))
}

/// Read the settings. A missing or broken file reads as no settings, because
/// the answer to both is the same: ask the person again.
pub fn load(app: &AppHandle) -> Option<Settings> {
    let path = match file_path(app) {
        Ok(path) => path,
        Err(err) => {
            eprintln!("teha: {err}");
            return None;
        }
    };
    let text = fs::read_to_string(&path).ok()?;
    match serde_json::from_str::<Settings>(&text) {
        Ok(settings) => Some(settings),
        Err(err) => {
            eprintln!("teha: {} does not parse: {err}", path.display());
            None
        }
    }
}

/// Write the settings file with an owner-only mode.
pub fn store(app: &AppHandle, settings: &Settings) -> Result<(), String> {
    let path = file_path(app)?;
    if let Some(dir) = path.parent() {
        fs::create_dir_all(dir).map_err(|err| format!("cannot make {}: {err}", dir.display()))?;
    }
    let text = serde_json::to_string_pretty(settings)
        .map_err(|err| format!("cannot write the settings: {err}"))?;

    let mut options = fs::OpenOptions::new();
    options.write(true).create(true).truncate(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }
    let mut file = options
        .open(&path)
        .map_err(|err| format!("cannot open {}: {err}", path.display()))?;
    file.write_all(text.as_bytes())
        .map_err(|err| format!("cannot write {}: {err}", path.display()))?;
    Ok(())
}

/// Read the device token for one server out of the credential store.
pub fn token(server: &str) -> Option<String> {
    let entry = match Entry::new(KEYCHAIN_SERVICE, server) {
        Ok(entry) => entry,
        Err(err) => {
            eprintln!("teha: the credential store is not reachable: {err}");
            return None;
        }
    };
    match entry.get_password() {
        Ok(secret) => Some(secret),
        Err(keyring::Error::NoEntry) => None,
        Err(err) => {
            eprintln!("teha: the credential store refused a read: {err}");
            None
        }
    }
}

/// Put the device token for one server into the credential store. An empty
/// token removes the entry.
pub fn set_token(server: &str, secret: &str) -> Result<(), String> {
    let entry = Entry::new(KEYCHAIN_SERVICE, server)
        .map_err(|err| format!("the credential store is not reachable: {err}"))?;
    if secret.is_empty() {
        return match entry.delete_credential() {
            Ok(()) | Err(keyring::Error::NoEntry) => Ok(()),
            Err(err) => Err(format!("the credential store refused a delete: {err}")),
        };
    }
    entry
        .set_password(secret)
        .map_err(|err| format!("the credential store refused a write: {err}"))
}

/// Reduce a typed address to an origin, and refuse anything that is not one.
///
/// The web app lives at the root of the server, so a path, a query and a
/// fragment are dropped rather than kept. A scheme other than http or https
/// is an error, because a window must never load a file or a data URL from a
/// setting.
pub fn normalise_server(raw: &str) -> Result<String, String> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return Err("the server address is empty".into());
    }
    let url = Url::parse(trimmed)
        .map_err(|_| "the server address must look like https://teha.example".to_string())?;
    if url.scheme() != "http" && url.scheme() != "https" {
        return Err("the address must start with http:// or https://".into());
    }
    let host = url.host_str().unwrap_or("");
    if host.is_empty() {
        return Err("the address has no host".into());
    }
    let mut origin = format!("{}://{host}", url.scheme());
    if let Some(port) = url.port() {
        origin.push_str(&format!(":{port}"));
    }
    Ok(origin)
}

/// Reduce a shortcut to a trimmed string, or to the default when it is empty.
/// The shell parses it later, and it falls back when the parse fails.
pub fn normalise_shortcut(raw: &str) -> String {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        DEFAULT_SHORTCUT.to_string()
    } else {
        trimmed.to_string()
    }
}

/// Reduce free text to one safe quick add line.
///
/// The text can arrive from a `teha://` URL, which is an input from outside
/// the application. So this takes the first line only, removes every control
/// character, trims, and caps the length. The result reaches a text field and
/// never a shell.
pub fn sanitise_line(raw: &str) -> Result<String, String> {
    let first = raw.split(['\n', '\r']).next().unwrap_or("");
    let cleaned: String = first
        .chars()
        .map(|c| if c == '\t' { ' ' } else { c })
        .filter(|c| !c.is_control())
        .collect();
    let capped: String = cleaned.chars().take(MAX_LINE).collect();
    let line = capped.trim();
    if line.is_empty() {
        return Err("the line has no text".into());
    }
    Ok(line.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn an_origin_survives_and_a_path_does_not() {
        assert_eq!(
            normalise_server(" https://teha.example/app?x=1#top "),
            Ok("https://teha.example".to_string())
        );
        assert_eq!(
            normalise_server("http://127.0.0.1:8637"),
            Ok("http://127.0.0.1:8637".to_string())
        );
        // The default port of the scheme is not repeated.
        assert_eq!(
            normalise_server("https://teha.example:443"),
            Ok("https://teha.example".to_string())
        );
    }

    #[test]
    fn only_http_and_https_are_addresses() {
        for bad in [
            "",
            "teha.example",
            "file:///etc/passwd",
            "javascript:alert(1)",
            "data:text/html,<b>",
            "http://",
        ] {
            assert!(normalise_server(bad).is_err(), "{bad} was accepted");
        }
    }

    #[test]
    fn a_line_loses_its_second_line_and_its_control_characters() {
        assert_eq!(sanitise_line("  Buy milk  "), Ok("Buy milk".to_string()));
        assert_eq!(
            sanitise_line("Buy milk\nCall the bank"),
            Ok("Buy milk".to_string())
        );
        assert_eq!(
            sanitise_line("Buy\tmilk\u{7}\u{1b}[2J"),
            Ok("Buy milk[2J".to_string())
        );
        assert!(sanitise_line("   ").is_err());
        assert!(sanitise_line("\u{0}").is_err());
    }

    #[test]
    fn a_long_line_is_capped() {
        let line = sanitise_line(&"a".repeat(MAX_LINE + 40)).unwrap();
        assert_eq!(line.chars().count(), MAX_LINE);
    }

    #[test]
    fn an_empty_shortcut_falls_back() {
        assert_eq!(normalise_shortcut("  "), DEFAULT_SHORTCUT);
        assert_eq!(normalise_shortcut(" Alt+Space "), "Alt+Space");
    }
}

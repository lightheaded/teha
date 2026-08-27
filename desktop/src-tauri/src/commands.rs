// SPDX-License-Identifier: Apache-2.0

//! The four commands that the two local pages call.
//!
//! The quick add panel and the settings window are pages this shell serves
//! itself, and the capability file names both. The page that comes from the
//! server is a remote origin, and no capability names it, so it reaches none
//! of these commands.

use tauri::AppHandle;

use crate::settings::{self, Settings, View};
use crate::{panel, setup, shortcut, web};

/// Add one task from the panel, then close the panel.
///
/// `Ok` means the page took the line, not that the task exists. The page of
/// the web app writes the task into its own store, so a failure after this
/// point is a failure of that page and it reports itself there.
#[tauri::command]
pub fn panel_submit(app: AppHandle, line: String) -> Result<(), String> {
    web::add_task(&app, &line)?;
    panel::hide(&app);
    Ok(())
}

/// Close the panel. `Escape` in the panel comes here.
#[tauri::command]
pub fn panel_close(app: AppHandle) {
    panel::hide(&app);
}

/// What the settings window shows. The token is never part of the answer.
#[tauri::command]
pub fn settings_read(app: AppHandle) -> View {
    let current = settings::load(&app);
    let server = current
        .as_ref()
        .map(|held| held.server.clone())
        .unwrap_or_default();
    let shortcut = current
        .as_ref()
        .map(|held| held.shortcut.clone())
        .unwrap_or_else(|| settings::DEFAULT_SHORTCUT.to_string());
    let has_token = !server.is_empty() && settings::token(&server).is_some();
    View {
        server,
        shortcut,
        has_token,
        default_shortcut: settings::DEFAULT_SHORTCUT.to_string(),
    }
}

/// Save the settings, then open the web app with them.
#[tauri::command]
pub fn settings_write(
    app: AppHandle,
    server: String,
    token: String,
    shortcut: String,
) -> Result<(), String> {
    let server = settings::normalise_server(&server)?;
    // A local name that is not "shortcut", so that the module of the same name
    // stays readable two lines down.
    let accelerator = settings::normalise_shortcut(&shortcut);

    // An empty token field keeps the token that the keychain holds. A person
    // who corrects the address alone must not have to find the token again.
    let secret = token.trim();
    if !secret.is_empty() {
        settings::set_token(&server, secret)?;
    } else if settings::token(&server).is_none() {
        return Err("this server has no token yet, so the field cannot be empty".into());
    }

    let saved = Settings {
        server,
        shortcut: accelerator.clone(),
    };
    settings::store(&app, &saved)?;
    shortcut::register(&app, &accelerator);

    // The old window holds the old address and the old cookie, so it goes.
    web::forget(&app);
    setup::close(&app);
    web::show(&app);
    Ok(())
}

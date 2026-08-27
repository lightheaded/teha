// SPDX-License-Identifier: Apache-2.0

//! The global shortcut.
//!
//! One shortcut, and it toggles the quick add panel. The string is an
//! accelerator, for example `CmdOrCtrl+Shift+A` or `Alt+Space`. A string that
//! does not parse falls back to the default, because a shell with no shortcut
//! is a failure a person cannot see.

use tauri::AppHandle;
use tauri_plugin_global_shortcut::{GlobalShortcutExt, ShortcutState};

use crate::panel;
use crate::settings;

/// Register the shortcut. It replaces the one that is registered already, so
/// a save in the settings window takes effect at once.
pub fn register(app: &AppHandle, wanted: &str) {
    if let Err(err) = app.global_shortcut().unregister_all() {
        eprintln!("teha: the old shortcut did not go: {err}");
    }
    if attach(app, wanted) {
        return;
    }
    if wanted != settings::DEFAULT_SHORTCUT {
        eprintln!(
            "teha: {wanted} did not register, so the default {} is used",
            settings::DEFAULT_SHORTCUT
        );
        attach(app, settings::DEFAULT_SHORTCUT);
    }
}

fn attach(app: &AppHandle, accelerator: &str) -> bool {
    let result = app
        .global_shortcut()
        .on_shortcut(accelerator, |app, _shortcut, event| {
            // The key press opens the panel. The release does nothing, or the
            // panel would open and close on one tap.
            if event.state == ShortcutState::Pressed {
                panel::toggle(app);
            }
        });
    match result {
        Ok(()) => true,
        Err(err) => {
            eprintln!("teha: the shortcut {accelerator} did not register: {err}");
            false
        }
    }
}

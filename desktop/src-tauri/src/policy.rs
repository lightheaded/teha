// SPDX-License-Identifier: Apache-2.0

//! The macOS Dock and keyboard behaviour.
//!
//! The shell is a regular application: it has a Dock icon from the first
//! second to the last, and a click on that icon opens the window that hosts the
//! web app. See the reopen event in `main.rs`.
//!
//! The quick add panel opens over another application, and that application
//! must get the keyboard back when the panel closes. A regular application
//! keeps the keyboard when its last window hides, so the shell hides itself
//! instead. macOS then activates the application that was in front before. The
//! hide is skipped while the web app or the settings window is on the screen,
//! because a person who works in one of those keeps it.
//!
//! Every function does nothing on Windows and on Linux, where the panel gives
//! the focus back on its own.

use tauri::AppHandle;
#[cfg(target_os = "macos")]
use tauri::Manager;

use crate::setup;
use crate::web;

/// Take a Dock icon and a place in the application switcher. Called once, at
/// the start.
pub fn regular(app: &AppHandle) {
    let _ = app;
    #[cfg(target_os = "macos")]
    if let Err(err) = app.set_activation_policy(tauri::ActivationPolicy::Regular) {
        eprintln!("teha: the activation policy did not become regular: {err}");
    }
}

/// Undo `release_keyboard`, before a window of the shell comes on the screen.
/// The call does not activate the application. The window that follows does.
pub fn unhide(app: &AppHandle) {
    let _ = app;
    #[cfg(target_os = "macos")]
    if let Err(err) = app.show() {
        eprintln!("teha: the application did not unhide: {err}");
    }
}

/// Give the keyboard back to the application in front of the shell, when no
/// window of the shell other than the panel is on the screen.
pub fn release_keyboard(app: &AppHandle) {
    let _ = app;
    #[cfg(target_os = "macos")]
    {
        let visible = |label: &str| {
            app.get_webview_window(label)
                .and_then(|window| window.is_visible().ok())
                .unwrap_or(false)
        };
        if visible(web::MAIN) || visible(setup::SETTINGS) {
            return;
        }
        if let Err(err) = app.hide() {
            eprintln!("teha: the application did not hide: {err}");
        }
    }
}

// SPDX-License-Identifier: Apache-2.0

//! The settings window.
//!
//! It asks for two things once: the address of the server and the device
//! token. The address goes into a file, the token goes into the keychain. The
//! page is local, because the shell cannot ask the server for anything before
//! it knows where the server is.

use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindowBuilder};

use crate::policy;

/// The label of the settings window. The capability file names it.
pub const SETTINGS: &str = "settings";

/// Open the settings window, or bring it to the front.
pub fn open(app: &AppHandle) {
    policy::regular(app);
    if let Some(window) = app.get_webview_window(SETTINGS) {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
        return;
    }

    let built = WebviewWindowBuilder::new(app, SETTINGS, WebviewUrl::App("settings.html".into()))
        .title("teha settings")
        .inner_size(520.0, 470.0)
        .resizable(false)
        .center()
        .build();
    if let Err(err) = built {
        eprintln!("teha: the settings window did not open: {err}");
    }
}

/// Close the settings window after a save.
pub fn close(app: &AppHandle) {
    if let Some(window) = app.get_webview_window(SETTINGS) {
        let _ = window.destroy();
    }
}

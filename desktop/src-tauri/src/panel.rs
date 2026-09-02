// SPDX-License-Identifier: Apache-2.0

//! The quick add panel.
//!
//! A small window with one field, on top of every other window. The global
//! shortcut opens it, `Escape` closes it and `Enter` adds the task and closes
//! it. The page is local, so it opens with no network call and no server.
//!
//! The panel must not take the place of the application the person works in.
//! On macOS the shell hides itself when the panel was its only window on the
//! screen, so the keyboard goes back to that application. See `policy.rs`.

use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindow, WebviewWindowBuilder, WindowEvent};

use crate::policy;

/// The label of the panel window. The capability file names it.
pub const PANEL: &str = "quickadd";

fn create(app: &AppHandle) -> Result<WebviewWindow, String> {
    let window = WebviewWindowBuilder::new(app, PANEL, WebviewUrl::App("quickadd.html".into()))
        .title("teha quick add")
        .inner_size(620.0, 112.0)
        .resizable(false)
        .decorations(false)
        .always_on_top(true)
        .skip_taskbar(true)
        .visible(false)
        .center()
        .build()
        .map_err(|err| format!("the quick add window did not open: {err}"))?;

    let handle = app.clone();
    window.on_window_event(move |event| match event {
        // A panel that stays on the screen after the person clicks somewhere
        // else is in the way. It goes when it loses the keyboard.
        WindowEvent::Focused(false) => hide(&handle),
        WindowEvent::CloseRequested { api, .. } => {
            api.prevent_close();
            hide(&handle);
        }
        _ => {}
    });

    Ok(window)
}

/// Hide the panel and give the keyboard back.
pub fn hide(app: &AppHandle) {
    if let Some(window) = app.get_webview_window(PANEL) {
        let _ = window.hide();
    }
    policy::release_keyboard(app);
}

/// Open the panel in the middle of the screen, with the field ready.
pub fn show(app: &AppHandle) {
    let window = match app.get_webview_window(PANEL) {
        Some(window) => window,
        None => match create(app) {
            Ok(window) => window,
            Err(err) => {
                eprintln!("teha: {err}");
                return;
            }
        },
    };
    policy::unhide(app);
    let _ = window.center();
    let _ = window.show();
    let _ = window.set_focus();
    // The page also acts on the focus event. This call is what makes the
    // second open as clean as the first: it clears the old line.
    let _ = window.eval("window.tehaOpen && window.tehaOpen()");
}

/// Open the panel, or close it when it is already open. The global shortcut
/// and the menu bar item both come here.
pub fn toggle(app: &AppHandle) {
    match app.get_webview_window(PANEL) {
        Some(window) if window.is_visible().unwrap_or(false) => hide(app),
        _ => show(app),
    }
}

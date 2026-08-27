// SPDX-License-Identifier: Apache-2.0

//! The macOS activation policy.
//!
//! An accessory application has no Dock icon and no place in the application
//! switcher. That is what lets the quick add panel open over another
//! application and give the keyboard back when it closes. The main window needs
//! the regular policy, because a window a person works in belongs in the Dock.
//!
//! Both functions do nothing on Windows and on Linux, where the panel gives
//! the focus back on its own.

use tauri::AppHandle;

/// Take a Dock icon, for the window that hosts the web app.
pub fn regular(app: &AppHandle) {
    let _ = app;
    #[cfg(target_os = "macos")]
    if let Err(err) = app.set_activation_policy(tauri::ActivationPolicy::Regular) {
        eprintln!("teha: the activation policy did not become regular: {err}");
    }
}

/// Give up the Dock icon, so that the panel does not take the place of the
/// application the person works in.
pub fn accessory(app: &AppHandle) {
    let _ = app;
    #[cfg(target_os = "macos")]
    if let Err(err) = app.set_activation_policy(tauri::ActivationPolicy::Accessory) {
        eprintln!("teha: the activation policy did not become accessory: {err}");
    }
}

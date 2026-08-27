// SPDX-License-Identifier: Apache-2.0

//! The menu bar icon and its menu.
//!
//! Three actions and a quit. The shell has no window of its own on the screen
//! most of the time, so this icon is the way back to it.

use tauri::image::Image;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::TrayIconBuilder;
use tauri::AppHandle;

use crate::panel;
use crate::setup;
use crate::web;

/// The menu bar image is a mask. macOS reads the alpha channel and paints the
/// colour itself, so one file follows a light and a dark menu bar.
/// `tools/make-icons.py` draws it.
const TRAY_PNG: &[u8] = include_bytes!("../icons/tray.png");

/// Put the icon in the menu bar. The shortcut goes into the label of the
/// first item, because that is where a person looks for it.
pub fn build(app: &AppHandle, shortcut: &str) -> tauri::Result<()> {
    let label = format!("Quick add ({shortcut})");
    let quick = MenuItem::with_id(app, "quick_add", &label, true, None::<&str>)?;
    let open = MenuItem::with_id(app, "open_main", "Open teha", true, None::<&str>)?;
    let settings = MenuItem::with_id(app, "settings", "Settings", true, None::<&str>)?;
    let line = PredefinedMenuItem::separator(app)?;
    let quit = PredefinedMenuItem::quit(app, Some("Quit teha"))?;
    let menu = Menu::with_items(app, &[&quick, &open, &settings, &line, &quit])?;

    TrayIconBuilder::with_id("teha")
        .icon(Image::from_bytes(TRAY_PNG)?)
        .icon_as_template(true)
        .tooltip("teha")
        .menu(&menu)
        .show_menu_on_left_click(true)
        .on_menu_event(|app, event| match event.id().as_ref() {
            "quick_add" => panel::show(app),
            "open_main" => web::show(app),
            "settings" => setup::open(app),
            other => eprintln!("teha: the menu item {other} has no action"),
        })
        .build(app)?;
    Ok(())
}

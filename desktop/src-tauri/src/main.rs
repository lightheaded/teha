// SPDX-License-Identifier: Apache-2.0

//! teha desktop: a small shell around the web app of your own server.
//!
//! What it adds to a browser tab:
//!
//! - a global shortcut that opens a quick add panel over every application,
//! - a menu bar icon,
//! - the `teha://` URL scheme, for Shortcuts, Raycast and Keyboard Maestro,
//! - one place for the server address, with the device token in the keychain.
//!
//! What it does not add: a second copy of the web app, and a second quick add
//! parser. The shell points a window at the server and drives the page that
//! arrives. See `web.rs`.
//!
//! No log line here holds a task, a URL or a token. A task title is personal
//! text, and a shell that prints it writes a person's life into a log file.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod commands;
mod panel;
mod policy;
mod scheme;
mod settings;
mod setup;
mod shortcut;
mod tray;
mod web;

use tauri::AppHandle;
use tauri_plugin_deep_link::DeepLinkExt;

fn main() {
    let mut builder = tauri::Builder::default();

    // The single instance plugin goes on first, which is what it asks for. A
    // second launch then reaches the running app rather than starting a rival
    // that holds a second copy of the same list.
    #[cfg(desktop)]
    {
        builder = builder.plugin(tauri_plugin_single_instance::init(|app, argv, _cwd| {
            let carries_url = argv
                .iter()
                .any(|arg| arg.trim_start().to_ascii_lowercase().starts_with("teha:"));
            if carries_url {
                // The deep link plugin hands the URL to on_open_url on its
                // own. A window would be in the way of a capture.
                eprintln!("teha: a second launch carried a URL");
                return;
            }
            eprintln!("teha: a second launch arrived, so the window comes forward");
            web::show(app);
        }));
    }

    builder
        .plugin(tauri_plugin_deep_link::init())
        .plugin(tauri_plugin_global_shortcut::Builder::new().build())
        .invoke_handler(tauri::generate_handler![
            commands::panel_submit,
            commands::panel_close,
            commands::settings_read,
            commands::settings_write,
        ])
        .setup(|app| {
            let handle = app.handle().clone();

            // A regular application, with a Dock icon at all times. A click on
            // the icon reaches the reopen event below.
            policy::regular(&handle);

            let current = settings::load(&handle);
            let accelerator = current
                .as_ref()
                .map(|held| held.shortcut.clone())
                .unwrap_or_else(|| settings::DEFAULT_SHORTCUT.to_string());

            tray::build(&handle, &accelerator)?;
            shortcut::register(&handle, &accelerator);

            // A URL can arrive at any moment, so this goes up before the
            // window does.
            let for_urls = handle.clone();
            app.deep_link().on_open_url(move |event| {
                for url in event.urls() {
                    act(&for_urls, url.as_str());
                }
            });

            if current.is_some() {
                // The window starts hidden and warm. A quick add then needs no
                // page load, and the outbox drains from the first minute.
                if let Err(err) = web::window(&handle) {
                    eprintln!("teha: {err}");
                }
            } else {
                setup::open(&handle);
            }

            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("the teha desktop shell did not start")
        .run(|app, event| {
            // A click on the Dock icon, or a second open from the Finder, while
            // no window of the shell is on the screen. A regular application
            // answers with its window, so this one answers with the web app.
            #[cfg(target_os = "macos")]
            if let tauri::RunEvent::Reopen {
                has_visible_windows: false,
                ..
            } = event
            {
                web::show(app);
            }
            #[cfg(not(target_os = "macos"))]
            let _ = (app, event);
        });
}

/// Act on one `teha://` URL.
///
/// The URL comes from outside the application, so the parser refuses anything
/// it does not know and the text reaches a text field rather than a shell. The
/// log line names the action and never the URL.
fn act(app: &AppHandle, raw: &str) {
    match scheme::parse(raw) {
        Ok(scheme::Action::Add(line)) => {
            if let Err(err) = web::add_task(app, &line) {
                eprintln!("teha: the URL did not add a task: {err}");
            }
        }
        Ok(scheme::Action::Unknown(action)) => {
            eprintln!("teha: the URL asks for \"{action}\", which this build does not answer");
        }
        Err(err) => eprintln!("teha: a teha:// URL was refused: {err}"),
    }
}

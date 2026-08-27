// SPDX-License-Identifier: Apache-2.0

//! The window that hosts the web app, and the one bridge into it.
//!
//! The shell holds no copy of the web app and no second quick add parser. It
//! points a window at the server, and the page that arrives is the page a
//! browser gets. The bridge into that page is `eval` from Rust, in two places
//! only: the sign in, and one quick add line.
//!
//! The page comes from a remote origin, so no capability names it. A page from
//! the server therefore reaches no command of this shell at all. The traffic
//! goes one way, from Rust into the page.

use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindow, WebviewWindowBuilder, WindowEvent};
use url::Url;

use crate::policy;
use crate::settings::{self, Settings};

/// The label of the window that hosts the web app.
pub const MAIN: &str = "main";

const LOGIN_JS: &str = include_str!("js/login.js");
const ADD_JS: &str = include_str!("js/add.js");

/// Turn a string into a JavaScript string literal.
///
/// This is what keeps a `teha://` URL out of the code of the page. JSON string
/// escaping is JavaScript string escaping, with one exception: JSON leaves the
/// two Unicode line separators raw, and JavaScript once read them as the end of
/// a line. They are escaped here as well.
fn js_literal(value: &str) -> String {
    let text = serde_json::to_string(value).unwrap_or_else(|_| "\"\"".to_string());
    text.replace('\u{2028}', "\\u2028")
        .replace('\u{2029}', "\\u2029")
}

/// Open the window that hosts the web app. It starts hidden, so that a task
/// from a `teha://` URL needs no window on the screen.
fn create(
    app: &AppHandle,
    settings: &Settings,
    token: Option<&str>,
) -> Result<WebviewWindow, String> {
    let url = settings
        .server
        .parse::<Url>()
        .map_err(|err| format!("the server address does not parse: {err}"))?;
    let script = LOGIN_JS.replace("__TEHA_TOKEN__", &js_literal(token.unwrap_or("")));

    let window = WebviewWindowBuilder::new(app, MAIN, WebviewUrl::External(url))
        .title("teha")
        .inner_size(1100.0, 760.0)
        .visible(false)
        .initialization_script(script)
        .build()
        .map_err(|err| format!("the main window did not open: {err}"))?;

    let handle = app.clone();
    window.on_window_event(move |event| {
        if let WindowEvent::CloseRequested { api, .. } = event {
            // Hide rather than close. The page holds the local rows and the
            // outbox, and it reconnects the event stream on every load, so a
            // real close makes the next open slow for no gain.
            api.prevent_close();
            if let Some(main) = handle.get_webview_window(MAIN) {
                let _ = main.hide();
            }
            policy::accessory(&handle);
        }
    });

    Ok(window)
}

/// Return the window that hosts the web app, and open it when it is absent.
pub fn window(app: &AppHandle) -> Result<WebviewWindow, String> {
    if let Some(window) = app.get_webview_window(MAIN) {
        return Ok(window);
    }
    let settings =
        settings::load(app).ok_or_else(|| "the server address is not set yet".to_string())?;
    let token = settings::token(&settings.server);
    create(app, &settings, token.as_deref())
}

/// Bring the web app to the front. A missing server address opens the settings
/// window instead, because that is the only thing a person can do about it.
pub fn show(app: &AppHandle) {
    match window(app) {
        Ok(window) => {
            policy::regular(app);
            let _ = window.unminimize();
            let _ = window.show();
            let _ = window.set_focus();
        }
        Err(err) => {
            eprintln!("teha: {err}");
            crate::setup::open(app);
        }
    }
}

/// Close the window that hosts the web app, so that the next open reads the
/// new settings and signs in again.
pub fn forget(app: &AppHandle) {
    if let Some(window) = app.get_webview_window(MAIN) {
        let _ = window.destroy();
    }
}

/// Put one quick add line into the quick add field of the web app.
///
/// The line reaches a text field, and never a shell. The web app parses it,
/// writes the task into its own store and its own outbox, and syncs it. So a
/// task from the panel travels the same path as a task a person types.
///
/// The window can be hidden. A hidden window still runs the script, which is
/// what lets a `teha://` URL add a task with nothing on the screen.
pub fn add_task(app: &AppHandle, line: &str) -> Result<(), String> {
    let line = settings::sanitise_line(line)?;
    let window = window(app)?;
    let script = ADD_JS.replace("__TEHA_LINE__", &js_literal(&line));
    window
        .eval(script)
        .map_err(|err| format!("the page did not take the line: {err}"))
}

#[cfg(test)]
mod tests {
    use super::js_literal;

    #[test]
    fn a_literal_cannot_end_the_script_around_it() {
        assert_eq!(js_literal("Buy milk"), "\"Buy milk\"");
        assert_eq!(
            js_literal("Ask \"why\" \\ now"),
            "\"Ask \\\"why\\\" \\\\ now\""
        );
        assert_eq!(js_literal("a</script>b"), "\"a</script>b\"");
        assert_eq!(js_literal("a\u{2028}b"), "\"a\\u2028b\"");
        assert_eq!(js_literal("a\u{2029}b"), "\"a\\u2029b\"");
        assert_eq!(js_literal("a\nb"), "\"a\\nb\"");
    }
}

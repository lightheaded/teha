// SPDX-License-Identifier: Apache-2.0

//! The `teha://` URL scheme.
//!
//! A URL is an input from outside the application. Apple Shortcuts, Raycast
//! and Keyboard Maestro all write one, and so can any web page. So this module
//! trusts nothing in it: it accepts one action, reads one field, and hands a
//! cleaned line to the caller. Nothing here builds a command line, and nothing
//! here reaches a shell.
//!
//! `teha://add?text=Buy%20milk%20tomorrow%20p1` adds a task.
//! Every other action logs a line and does nothing.

use url::Url;

use crate::settings;

/// What a `teha://` URL asks for.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Action {
    /// Add one task from a quick add line.
    Add(String),
    /// An action this build does not answer. The string is the action name,
    /// for the log line.
    Unknown(String),
}

/// Read a `teha://` URL.
///
/// The action is the part after the scheme, whether a person writes
/// `teha://add?...` or `teha:add?...`. In the first form a URL parser calls
/// `add` the host, and in the second it calls it the path, so both are read.
pub fn parse(raw: &str) -> Result<Action, String> {
    let url = Url::parse(raw.trim()).map_err(|err| format!("the URL does not parse: {err}"))?;
    if url.scheme() != "teha" {
        return Err(format!("the scheme is {}, not teha", url.scheme()));
    }

    let action = match url.host_str() {
        Some(host) if !host.is_empty() => host.to_ascii_lowercase(),
        _ => url
            .path()
            .trim_matches('/')
            .split('/')
            .next()
            .unwrap_or("")
            .to_ascii_lowercase(),
    };

    if action != "add" {
        return Ok(Action::Unknown(action));
    }

    // query_pairs percent-decodes and turns a plus into a space, which is what
    // every launcher writes.
    let text = url
        .query_pairs()
        .find(|(key, _)| key == "text")
        .map(|(_, value)| value.into_owned())
        .ok_or_else(|| "teha://add needs a text field".to_string())?;

    Ok(Action::Add(settings::sanitise_line(&text)?))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn add(raw: &str) -> String {
        match parse(raw) {
            Ok(Action::Add(line)) => line,
            other => panic!("{raw} gave {other:?}"),
        }
    }

    #[test]
    fn a_percent_encoded_line_arrives_whole() {
        assert_eq!(
            add("teha://add?text=Book%20the%20ferry%20tomorrow%20at%209%3A30%20p1%20%23Trip"),
            "Book the ferry tomorrow at 9:30 p1 #Trip"
        );
    }

    #[test]
    fn a_plus_is_a_space() {
        assert_eq!(add("teha://add?text=Buy+oat+milk"), "Buy oat milk");
    }

    #[test]
    fn the_two_forms_of_the_action_both_read() {
        assert_eq!(add("teha:add?text=Buy%20milk"), "Buy milk");
        assert_eq!(add("teha://add/?text=Buy%20milk"), "Buy milk");
        assert_eq!(add("TEHA://ADD?text=Buy%20milk"), "Buy milk");
    }

    #[test]
    fn other_fields_and_other_order_do_not_matter() {
        assert_eq!(
            add("teha://add?source=raycast&text=Order%20gravel%20%23Garden"),
            "Order gravel #Garden"
        );
    }

    #[test]
    fn an_estonian_project_name_survives() {
        assert_eq!(
            add("teha://add?text=Osta%20piima%20%23K%C3%B5rvalprojekt"),
            "Osta piima #Kõrvalprojekt"
        );
    }

    #[test]
    fn an_unknown_action_is_named_and_not_run() {
        // PLAN.md section 13 proposes teha://run one day. It is not decided,
        // so this build logs it and does nothing.
        assert_eq!(
            parse("teha://run?task=t_M0X40RK3002RHMA"),
            Ok(Action::Unknown("run".to_string()))
        );
        assert_eq!(parse("teha://"), Ok(Action::Unknown(String::new())));
    }

    #[test]
    fn another_scheme_is_refused() {
        assert!(parse("https://teha.example/add?text=x").is_err());
        assert!(parse("tehax://add?text=x").is_err());
        assert!(parse("not a url").is_err());
    }

    #[test]
    fn add_without_text_is_an_error_and_not_an_empty_task() {
        assert!(parse("teha://add").is_err());
        assert!(parse("teha://add?text=").is_err());
        assert!(parse("teha://add?text=%20%20").is_err());
    }

    #[test]
    fn a_line_break_in_the_field_does_not_smuggle_a_second_task() {
        assert_eq!(
            add("teha://add?text=Buy%20milk%0ACall%20the%20bank"),
            "Buy milk"
        );
    }

    #[test]
    fn a_quote_and_a_backslash_stay_text() {
        assert_eq!(
            add("teha://add?text=Ask%20%22why%22%20%5C%20now"),
            "Ask \"why\" \\ now"
        );
    }
}

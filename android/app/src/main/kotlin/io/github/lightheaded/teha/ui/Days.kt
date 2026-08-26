// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui

import java.time.DayOfWeek
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.time.format.TextStyle
import java.time.temporal.TemporalAdjusters
import java.util.Locale

/**
 * Days holds every rule about printing and choosing a day.
 *
 * One file, because the list screen and the detail screen must never disagree
 * about what "This weekend" means or how a due date reads. Two copies of these
 * rules drift the moment one screen changes.
 */

/** parseDay reads an ISO day, and returns null for anything else. */
internal fun parseDay(iso: String?): LocalDate? =
    if (iso.isNullOrEmpty()) null else runCatching { LocalDate.parse(iso) }.getOrNull()

/** The long form, for a chip or a menu: "Wed 26 Aug". */
internal val dayFormat: DateTimeFormatter =
    DateTimeFormatter.ofPattern("EEE d MMM", Locale.getDefault())

/**
 * dayChoices are the five presets that both screens offer.
 *
 * Each one carries the day it resolves to, so a menu can print it. A label
 * with no date behind it makes the user guess. A null day means "no date".
 */
internal fun dayChoices(today: LocalDate): List<Pair<String, LocalDate?>> = listOf(
    "Today" to today,
    "Tomorrow" to today.plusDays(1),
    "This weekend" to today.with(TemporalAdjusters.nextOrSame(DayOfWeek.SATURDAY)),
    // next, not nextOrSame: on a Monday "next week" means the Monday after
    // this one, and a choice that resolves to today is already above.
    "Next week" to today.with(TemporalAdjusters.next(DayOfWeek.MONDAY)),
    "No date" to null,
)

/** isOverdue is true when a day has passed. */
internal fun isOverdue(due: String?, today: String): Boolean = due != null && due < today

/** dueLabel prints a day the way a phone user reads it, not as an ISO string. */
internal fun dueLabel(due: String, time: String?, today: String): String {
    val day = parseDay(due) ?: return due
    val now = parseDay(today) ?: return due
    val name = when {
        day == now -> "today"
        day == now.plusDays(1) -> "tomorrow"
        day == now.minusDays(1) -> "yesterday"
        day.isBefore(now) && day.isAfter(now.minusDays(7)) ->
            day.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.getDefault())
        day.year == now.year -> day.format(DateTimeFormatter.ofPattern("d MMM"))
        else -> due
    }
    return if (time.isNullOrEmpty()) name else "$name $time"
}

/**
 * whenWord names a day inside a sentence.
 *
 * "today" reads as English in the middle of a sentence and a formatted date
 * does not, so only the two relative words are lower case.
 */
internal fun whenWord(iso: String, today: String): String {
    val day = parseDay(iso) ?: return iso
    val now = parseDay(today) ?: return iso
    return when {
        day == now -> "today"
        day == now.plusDays(1) -> "tomorrow"
        else -> day.format(dayFormat)
    }
}

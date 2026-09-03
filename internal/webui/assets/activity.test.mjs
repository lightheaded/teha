// SPDX-License-Identifier: AGPL-3.0-or-later

import test from 'node:test';
import assert from 'node:assert/strict';
import * as act from './activity.js';

// The store writes the fact and this file writes the sentence. What is worth a
// test is the mapping: an action a person can read, a field list a person can
// read, and the one command that puts a deleted row back.

test('every action a command can write has words', () => {
  // These are the command types of internal/store/commands.go, plus the
  // security actions of internal/store/activity.go. A new command with no
  // words here reads as its own type, which is a gap and not a failure, so the
  // test names them all rather than trusting the fallback.
  const actions = [
    'task_add', 'task_update', 'task_complete', 'task_wont_do', 'task_uncomplete',
    'task_delete', 'task_restore', 'task_move',
    'project_add', 'project_update', 'project_delete', 'project_restore',
    'section_add', 'section_update', 'section_move', 'section_reorder',
    'section_delete', 'section_restore',
    'comment_add', 'comment_update', 'comment_delete',
    'label_add', 'label_delete',
    'reminder_add', 'reminder_update', 'reminder_delete',
    'login', 'login_failed', 'logout', 'passkey_add', 'passkey_delete',
    'invite_create', 'invite_revoke', 'joined', 'share', 'unshare',
  ];
  for (const a of actions) {
    assert.notEqual(act.verb(a), a, `${a} reads as its own type`);
  }
});

test('an unknown action still reads as words', () => {
  assert.equal(act.verb('widget_add'), 'widget add');
});

test('the field list of an update reads as a sentence', () => {
  assert.equal(act.detailWords(''), '');
  assert.equal(act.detailWords('priority'), 'the priority');
  assert.equal(act.detailWords('due_date,priority'), 'the due date and the priority');
  assert.equal(act.detailWords('due_date,labels,priority'),
    'the due date, the labels and the priority');
  // A field the map does not know still reads, without an underscore.
  assert.equal(act.detailWords('source_ref'), 'source ref');
});

test('only a delete that has an undo offers one', () => {
  assert.equal(act.undoOf('task_delete'), 'task_restore');
  assert.equal(act.undoOf('project_delete'), 'project_restore');
  assert.equal(act.undoOf('section_delete'), 'section_restore');
  // The server has no comment_restore, so the log must not promise one.
  assert.equal(act.undoOf('comment_delete'), '');
  assert.equal(act.undoOf('task_add'), '');
});

test('a security line is told apart from a product line', () => {
  assert.equal(act.isSecurity('login_failed'), true);
  assert.equal(act.isSecurity('passkey_add'), true);
  assert.equal(act.isSecurity('task_add'), false);
  // Sharing a list is not a security line: it belongs to the list, and
  // everybody who can see the list reads it.
  assert.equal(act.isSecurity('share'), false);
});

test('the age of a line reads in the fewest words', () => {
  const now = Date.now();
  assert.equal(act.ago(new Date(now - 10 * 1000).toISOString()), 'now');
  assert.equal(act.ago(new Date(now - 5 * 60000).toISOString()), '5m ago');
  assert.equal(act.ago(new Date(now - 3 * 3600000).toISOString()), '3h ago');
  assert.equal(act.ago(new Date(now - 2 * 86400000).toISOString()), '2d ago');
  assert.equal(act.ago(''), '');
});

// SPDX-License-Identifier: AGPL-3.0-or-later

// A small Markdown renderer for a note and a task title.
//
// The app embeds every asset, and the content security policy in
// internal/webui/webui.go allows no third-party script, so this file is the
// whole renderer. It reads a useful subset and nothing more:
//
//   headings, a fenced code block, a blockquote, a bullet list, a numbered
//   list, a task list, a rule, a paragraph
//   **bold**, *italic*, `code`, ~~strike~~, [text](url), a bare URL
//
// Two rules keep it safe:
//
//   1. The text is escaped first. Every tag this file writes, it wrote itself,
//      so no markup in a note can reach the page.
//   2. A link target must carry a scheme the app trusts. A `javascript:` or a
//      `data:` target becomes plain text, not a link.
//
// One deliberate difference from CommonMark: a single newline inside a
// paragraph is a line break. A note is not prose, and a person who presses
// Enter once means a new line. `parser-fixtures/markdown.json` holds the case.

// An image is rendered as a link, not as an image. `img-src 'self' data:`
// blocks a remote picture, so an <img> tag would draw a broken frame. A link
// says what it is and it opens.

const SAFE = /^(?:https?:|mailto:|teha:)/i;

export function escapeHTML(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

// href returns a target the page can trust, or an empty string.
// A relative target that starts with "/" or "#" stays inside the app.
export function href(raw) {
  const u = String(raw || '').trim();
  if (!u) return '';
  if (u.startsWith('/') || u.startsWith('#')) return u;
  if (SAFE.test(u)) return u;
  return '';
}

// isURL says whether the whole string is one link the app trusts. The paste
// handler asks this before it turns a selection into a link.
export function isURL(s) {
  const u = String(s || '').trim();
  return !!u && !/\s/.test(u) && SAFE.test(u) && href(u) === u;
}

// --- inline -----------------------------------------------------------------
// Every step that writes a tag parks the tag in a slot and leaves a marker in
// the text. A later step therefore cannot read the inside of a link, so a URL
// in an href is never linked a second time.

const MARK = '\u0000';

function inlineWith(src, slots) {
  const put = (html) => { slots.push(html); return MARK + (slots.length - 1) + MARK; };
  let s = escapeHTML(src);

  // `code` first, so a star or a bracket inside it stays literal.
  s = s.replace(/`([^`\n]+)`/g, (_, code) => put(`<code>${code}</code>`));

  // ![alt](url) and [text](url). The image comes first, because its pattern is
  // the link pattern with one character in front.
  s = s.replace(/!\[([^\]\n]*)\]\(([^)\s]+)\)/g, (m, alt, url) => {
    const to = href(url);
    if (!to) return m;
    return put(anchor(to, emphasis(alt) || escapeHTML(url)));
  });
  s = s.replace(/\[([^\]\n]+)\]\(([^)\s]+)\)/g, (m, text, url) => {
    const to = href(url);
    if (!to) return m;
    return put(anchor(to, emphasis(text)));
  });

  // A bare URL. The trailing character class keeps a sentence's full stop or a
  // closing bracket out of the link.
  s = s.replace(/(^|[\s(])(https?:\/\/[^\s<]*[^\s<.,:;!?)\]])/g,
    (_, lead, url) => lead + put(anchor(url, escapeHTML(url))));

  s = emphasis(s);

  // A slot can hold another slot: `[a `b` c](url)` parks the code span first.
  for (let i = 0; i < 6 && s.includes(MARK); i++) {
    s = s.replace(new RegExp(MARK + '(\\d+)' + MARK, 'g'), (m, n) => slots[Number(n)] ?? m);
  }
  return s;
}

// anchor builds one link. to and text are escaped already, because every
// caller works on a string that went through escapeHTML first. Escaping the
// target a second time would write &amp;amp; into an href.
function anchor(to, text) {
  // A note can link anywhere, so a new tab and no referrer is the safe pair.
  const out = to.startsWith('/') || to.startsWith('#')
    ? `<a href="${to}">`
    : `<a href="${to}" target="_blank" rel="noopener noreferrer">`;
  return out + text + '</a>';
}

// emphasis applies the paired markers to text that is escaped already.
//
// A marker binds to a word, not to a space: `2 * 3 * 4` is arithmetic and it
// stays as it is. So the run inside a pair must start and end with a character
// that is not a space.
const RUN = '([^*_~\\s][^\\n]*?[^*_~\\s]|[^*_~\\s])';

function emphasis(s) {
  return String(s)
    .replace(new RegExp('\\*\\*' + RUN + '\\*\\*', 'g'), '<strong>$1</strong>')
    .replace(new RegExp('__' + RUN + '__', 'g'), '<strong>$1</strong>')
    .replace(new RegExp('~~' + RUN + '~~', 'g'), '<del>$1</del>')
    .replace(new RegExp('(^|[^*\\w])\\*' + RUN + '\\*(?![*\\w])', 'g'), '$1<em>$2</em>')
    .replace(new RegExp('(^|[^_\\w])_' + RUN + '_(?![_\\w])', 'g'), '$1<em>$2</em>');
}

// inline renders one line with no block structure. A task title uses it, so a
// row can carry a link without the row growing a paragraph.
export function inline(src) {
  if (!src) return '';
  return inlineWith(String(src).replace(/\n/g, ' '), []);
}

// --- blocks -----------------------------------------------------------------

const RE_FENCE = /^ {0,3}(`{3,}|~{3,})\s*(\S*)\s*$/;
const RE_HEAD = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const RE_RULE = /^ {0,3}(?:-{3,}|\*{3,}|_{3,})\s*$/;
const RE_QUOTE = /^ {0,3}>\s?(.*)$/;
const RE_ITEM = /^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$/;
const RE_TASK = /^\[([ xX])\]\s+(.*)$/;

// render turns a note into HTML.
export function render(src) {
  if (!src) return '';
  return blocks(String(src).replace(/\r\n?/g, '\n').split('\n'));
}

function blocks(lines) {
  const out = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];

    if (!line.trim()) { i++; continue; }

    const fence = line.match(RE_FENCE);
    if (fence) {
      const body = [];
      i++;
      while (i < lines.length && !new RegExp('^ {0,3}' + fence[1][0] + '{3,}\\s*$').test(lines[i])) {
        body.push(lines[i]); i++;
      }
      i++; // the closing fence, or the end of the note
      out.push(`<pre><code>${escapeHTML(body.join('\n'))}</code></pre>`);
      continue;
    }

    if (RE_RULE.test(line)) { out.push('<hr>'); i++; continue; }

    const head = line.match(RE_HEAD);
    if (head) {
      const n = head[1].length;
      out.push(`<h${n}>${inline(head[2])}</h${n}>`);
      i++;
      continue;
    }

    if (RE_QUOTE.test(line)) {
      const body = [];
      while (i < lines.length && RE_QUOTE.test(lines[i])) { body.push(lines[i].match(RE_QUOTE)[1]); i++; }
      out.push(`<blockquote>${blocks(body)}</blockquote>`);
      continue;
    }

    if (RE_ITEM.test(line)) {
      const [html, next] = list(lines, i, indentOf(lines[i]));
      out.push(html);
      i = next;
      continue;
    }

    // A paragraph runs to the next blank line or to the next block.
    const body = [];
    while (i < lines.length && lines[i].trim() && !isBlockStart(lines[i])) { body.push(lines[i].trim()); i++; }
    out.push(`<p>${body.map(inline).join('<br>')}</p>`);
  }
  return out.join('');
}

function isBlockStart(line) {
  return RE_FENCE.test(line) || RE_RULE.test(line) || RE_HEAD.test(line)
    || RE_QUOTE.test(line) || RE_ITEM.test(line);
}

function indentOf(line) {
  return (line.match(/^\s*/) || [''])[0].replace(/\t/g, '  ').length;
}

// list reads every item at one indent, and recurses for a deeper one. It
// returns the HTML and the line after the list.
function list(lines, start, indent) {
  const first = lines[start].match(RE_ITEM);
  const ordered = /\d/.test(first[2]);
  const items = [];
  let i = start;

  while (i < lines.length) {
    const m = lines[i].match(RE_ITEM);
    if (!m || indentOf(lines[i]) < indent) break;
    if (indentOf(lines[i]) > indent) {
      const [html, next] = list(lines, i, indentOf(lines[i]));
      items[items.length - 1] += html;
      i = next;
      continue;
    }
    if (/\d/.test(m[2]) !== ordered) break;
    const task = m[3].match(RE_TASK);
    if (task) {
      // The box is a picture of the state, not a control. Ticking it in the
      // note is in docs/BACKLOG.md.
      const done = task[1].toLowerCase() === 'x';
      items.push(`<span class="md-box">${done ? '&#9745;' : '&#9744;'}</span>`
        + `<span class="${done ? 'md-done' : ''}">${inline(task[2])}</span>`);
    } else {
      items.push(inline(m[3]));
    }
    i++;
  }

  const tag = ordered ? 'ol' : 'ul';
  const cls = items.some((x) => x.startsWith('<span class="md-box">')) ? ' class="md-tasks"' : '';
  return [`<${tag}${cls}>` + items.map((x) => `<li>${x}</li>`).join('') + `</${tag}>`, i];
}

// --- pasting a link ----------------------------------------------------------

// linkPaste answers what a paste should write into a text field. It returns
// null when the paste is an ordinary one, so the browser handles it.
//
// A URL pasted over selected text makes a Markdown link of that text. A URL
// pasted over a selection that is itself a URL replaces it, because two links
// is not what the person meant.
export function linkPaste(value, start, end, pasted) {
  const url = String(pasted || '').trim();
  if (!isURL(url)) return null;
  const selected = value.slice(start, end).trim();
  if (!selected || isURL(selected)) return null;
  // Selected text that already sits inside a link would nest one link in
  // another, so leave that paste alone.
  if (/[[\]()]/.test(selected)) return null;
  const text = `[${selected}](${url})`;
  return { value: value.slice(0, start) + text + value.slice(end), caret: start + text.length };
}

// wirePasteLink puts linkPaste on one field. The field keeps its own undo
// history, because the write goes through execCommand where the browser has
// one, and through the value only when it has not.
export function wirePasteLink(el, after) {
  el.addEventListener('paste', (e) => {
    const clip = e.clipboardData && e.clipboardData.getData('text/plain');
    const next = linkPaste(el.value, el.selectionStart, el.selectionEnd, clip);
    if (!next) return;
    e.preventDefault();
    const text = next.value.slice(el.selectionStart, next.caret);
    if (!document.execCommand || !document.execCommand('insertText', false, text)) {
      el.value = next.value;
      el.setSelectionRange(next.caret, next.caret);
    }
    if (after) after();
  });
}

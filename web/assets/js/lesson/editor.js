// Makes the exercise textarea behave enough like a code editor to type Swift in.
//
// Deliberately not CodeMirror. Its published ESM entry imports seven further
// modules from the CDN, so vendoring it self-contained — this project's rule for
// third-party JavaScript — would mean adding npm and a bundler to a repository
// that has no JavaScript build step at all. That is a large change to the shape
// of the project in exchange for syntax highlighting in one box.
//
// What is here instead covers what actually hurts when typing code into a plain
// textarea: Tab moving focus, and every new line starting at column zero.

const INDENT = '    '; // Swift's convention, and what the lesson bodies use.

function setupEditor(textarea) {
  if (textarea.dataset.editorReady) return;
  textarea.dataset.editorReady = 'true';

  // Tab inside a textarea is a genuine accessibility problem: a keyboard user
  // who lands here must still be able to leave. Escape releases the trap for
  // the next keypress, which is the convention screen-reader users expect, and
  // the hint below says so in text rather than leaving it to be discovered.
  let tabReleased = false;

  textarea.addEventListener('keydown', (event) => {
    // Checked first: a plain-Enter handler below returns early, so testing for
    // the modifier afterwards would never be reached.
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      textarea.form?.requestSubmit();
      return;
    }

    if (event.key === 'Escape') {
      tabReleased = true;
      return;
    }

    if (event.key === 'Tab') {
      if (tabReleased) {
        tabReleased = false;
        return; // let focus move on
      }
      event.preventDefault();
      event.shiftKey ? dedent(textarea) : indent(textarea);
      return;
    }

    tabReleased = false;

    if (event.key === 'Enter') {
      event.preventDefault();
      newlineKeepingIndent(textarea);
    }
  });
}

// replaceSelection edits through the undo stack rather than by assigning to
// .value, which would discard it — losing undo in a code editor is worse than
// having no editor features at all.
function replaceSelection(textarea, text) {
  textarea.focus();
  if (!document.execCommand || !document.execCommand('insertText', false, text)) {
    // Fallback for browsers that have dropped execCommand. Undo is lost here,
    // which is why it is the fallback and not the path.
    const { selectionStart: start, selectionEnd: end, value } = textarea;
    textarea.value = value.slice(0, start) + text + value.slice(end);
    textarea.selectionStart = textarea.selectionEnd = start + text.length;
  }
}

function currentLine(textarea) {
  const start = textarea.value.lastIndexOf('\n', textarea.selectionStart - 1) + 1;
  let end = textarea.value.indexOf('\n', textarea.selectionStart);
  if (end === -1) end = textarea.value.length;
  return { start, end, text: textarea.value.slice(start, end) };
}

function indent(textarea) {
  replaceSelection(textarea, INDENT);
}

function dedent(textarea) {
  const line = currentLine(textarea);
  const leading = line.text.match(/^ +/);
  if (!leading) return;

  const remove = Math.min(INDENT.length, leading[0].length);
  const caret = textarea.selectionStart;

  textarea.setSelectionRange(line.start, line.start + remove);
  replaceSelection(textarea, '');
  textarea.setSelectionRange(Math.max(line.start, caret - remove), Math.max(line.start, caret - remove));
}

// Enter keeps the current line's indentation, and adds one level after a line
// ending in an opening brace. Without this, every line of a nested example has
// to be indented by hand, which is the single most tiring thing about writing
// code in a textarea.
function newlineKeepingIndent(textarea) {
  const line = currentLine(textarea);
  const beforeCaret = line.text.slice(0, textarea.selectionStart - line.start);

  const leading = beforeCaret.match(/^ */)[0];
  const opensBlock = /[{([]\s*$/.test(beforeCaret.trimEnd());
  const closesNext = /^\s*[)\]}]/.test(textarea.value.slice(textarea.selectionStart));

  let insert = '\n' + leading + (opensBlock ? INDENT : '');

  // Typing Enter between a brace pair puts the closing brace on its own line,
  // indented to match the opening one.
  if (opensBlock && closesNext) {
    insert += '\n' + leading;
    replaceSelection(textarea, insert);
    const caret = textarea.selectionStart - (leading.length + 1);
    textarea.setSelectionRange(caret, caret);
    return;
  }

  replaceSelection(textarea, insert);
}

function init() {
  document.querySelectorAll('textarea[data-editor]').forEach(setupEditor);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}

// htmx swaps the result panel, and may later swap the form itself. Re-running
// init is safe: setupEditor is guarded against binding twice.
document.body?.addEventListener('htmx:afterSwap', init);

import { Extension } from "@tiptap/core";
import type { Editor } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";

type Range = { from: number; to: number } | null;
const key = new PluginKey<Range>("issueHighlight");

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    issueHighlight: {
      /** Mark a range of the document as the issue under discussion. */
      setIssueHighlight: (from: number, to: number) => ReturnType;
      clearIssueHighlight: () => ReturnType;
    };
  }
}

/** IssueHighlight paints one range with <mark class="issue-highlight"> without touching the document. */
export const IssueHighlight = Extension.create({
  name: "issueHighlight",
  addCommands() {
    return {
      setIssueHighlight:
        (from, to) =>
        ({ tr, dispatch }) => {
          if (dispatch) dispatch(tr.setMeta(key, { from, to }));
          return true;
        },
      clearIssueHighlight:
        () =>
        ({ tr, dispatch }) => {
          if (dispatch) dispatch(tr.setMeta(key, null));
          return true;
        },
    };
  },
  addProseMirrorPlugins() {
    return [
      new Plugin<Range>({
        key,
        state: {
          init: (): Range => null,
          apply(tr, prev): Range {
            const meta = tr.getMeta(key);
            if (meta !== undefined) return meta as Range;
            if (prev && tr.docChanged) {
              return { from: tr.mapping.map(prev.from), to: tr.mapping.map(prev.to) };
            }
            return prev;
          },
        },
        props: {
          decorations(state) {
            const range = key.getState(state);
            if (!range || range.from >= range.to) return DecorationSet.empty;
            return DecorationSet.create(state.doc, [Decoration.inline(range.from, range.to, { nodeName: "mark", class: "issue-highlight" })]);
          },
        },
      }),
    ];
  },
});

/** normalise mirrors the server's comparison form closely enough to find a quote the writer tidied up. */
function normalise(s: string): { text: string; map: number[] } {
  let text = "";
  const map: number[] = [];
  let lastSpace = true;
  for (let i = 0; i < s.length; i++) {
    const ch = s[i];
    let out = ch;
    if ("‘’‚′".includes(ch)) out = "'";
    else if ("“”„″«»".includes(ch)) out = '"';
    else if ("–—‒―−".includes(ch)) out = "-";
    else if (ch === "…") out = "...";
    else if (/\s/.test(ch) || ch === " ") out = " ";
    if (out === " ") {
      if (lastSpace) continue;
      lastSpace = true;
    } else {
      lastSpace = false;
    }
    for (const c of out) {
      text += c;
      map.push(i);
    }
  }
  while (text.endsWith(" ")) {
    text = text.slice(0, -1);
    map.pop();
  }
  return { text, map };
}

/**
 * findQuote locates a quote in the editor document and returns ProseMirror
 * positions. It searches each text block, exactly first and then normalised,
 * so curly quotes, dashes and whitespace differences still land.
 */
export function findQuote(editor: Editor, quote: string): { from: number; to: number } | null {
  const q = quote.trim();
  if (q.length < 3) return null;
  const nq = normalise(q).text.toLowerCase();
  let found: { from: number; to: number } | null = null;
  editor.state.doc.descendants((node, pos) => {
    if (found || !node.isTextblock) return !found;
    const text = node.textContent;
    const exact = text.indexOf(q);
    if (exact >= 0) {
      found = { from: pos + 1 + exact, to: pos + 1 + exact + q.length };
      return false;
    }
    const n = normalise(text);
    const idx = n.text.toLowerCase().indexOf(nq);
    if (idx >= 0) {
      const start = n.map[idx];
      const end = n.map[idx + nq.length - 1] + 1;
      found = { from: pos + 1 + start, to: pos + 1 + end };
      return false;
    }
    return true;
  });
  return found;
}

/** highlightQuote finds, marks and scrolls to a quote. It reports whether the quote was found. */
export function highlightQuote(editor: Editor, quote: string): boolean {
  const range = findQuote(editor, quote);
  if (!range) {
    editor.commands.clearIssueHighlight();
    return false;
  }
  editor.chain().setIssueHighlight(range.from, range.to).setTextSelection(range).scrollIntoView().run();
  const dom = editor.view.domAtPos(range.from).node;
  const el = dom instanceof Element ? dom : dom.parentElement;
  el?.scrollIntoView({ block: "center", behavior: "smooth" });
  return true;
}

import { useEffect } from "react";
import { EditorContent, useEditor, type Editor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { Button } from "../ui";
import { IssueHighlight } from "./issueHighlight";

type Props = {
  /** Markdown as loaded from the server. Changing the key remounts the editor. */
  initialMarkdown: string;
  /** Called once with the normalised markdown right after the editor mounts. */
  onReady: (editor: Editor, markdown: string) => void;
  onChange: (markdown: string) => void;
};

/** ChapterEditor is the TipTap editor with Markdown in and out. */
export function ChapterEditor({ initialMarkdown, onReady, onChange }: Props) {
  const editor = useEditor({
    extensions: [StarterKit, Markdown, IssueHighlight],
    content: initialMarkdown,
    contentType: "markdown",
    editorProps: {
      attributes: { class: "editor-content", spellcheck: "true" },
    },
    onUpdate: ({ editor }) => onChange(editor.getMarkdown()),
  });

  useEffect(() => {
    if (editor) onReady(editor, editor.getMarkdown());
    // onReady must run once per editor instance
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editor]);

  if (!editor) return null;
  return (
    <div>
      <Toolbar editor={editor} />
      <div className="rounded-b-lg border border-t-0 border-stone-200 bg-white px-8 py-6 shadow-sm">
        <EditorContent editor={editor} />
      </div>
    </div>
  );
}

function Toolbar({ editor }: { editor: Editor }) {
  const item = (label: string, active: boolean, run: () => void, title: string) => (
    <button
      type="button"
      title={title}
      onMouseDown={(e) => {
        e.preventDefault();
        run();
      }}
      className={`rounded px-2 py-1 text-sm ${active ? "bg-stone-900 text-white" : "text-stone-700 hover:bg-stone-200"}`}
    >
      {label}
    </button>
  );
  return (
    <div className="flex flex-wrap items-center gap-1 rounded-t-lg border border-stone-200 bg-stone-50 px-2 py-1.5">
      {item("B", editor.isActive("bold"), () => editor.chain().focus().toggleBold().run(), "Bold")}
      {item("I", editor.isActive("italic"), () => editor.chain().focus().toggleItalic().run(), "Italic")}
      <span className="mx-1 h-5 w-px bg-stone-300" />
      {item("H1", editor.isActive("heading", { level: 1 }), () => editor.chain().focus().toggleHeading({ level: 1 }).run(), "Heading 1")}
      {item("H2", editor.isActive("heading", { level: 2 }), () => editor.chain().focus().toggleHeading({ level: 2 }).run(), "Heading 2")}
      {item("H3", editor.isActive("heading", { level: 3 }), () => editor.chain().focus().toggleHeading({ level: 3 }).run(), "Heading 3")}
      <span className="mx-1 h-5 w-px bg-stone-300" />
      {item("• List", editor.isActive("bulletList"), () => editor.chain().focus().toggleBulletList().run(), "Bullet list")}
      {item("1. List", editor.isActive("orderedList"), () => editor.chain().focus().toggleOrderedList().run(), "Numbered list")}
      {item("❝ Quote", editor.isActive("blockquote"), () => editor.chain().focus().toggleBlockquote().run(), "Block quote")}
      {item("* * *", false, () => editor.chain().focus().setHorizontalRule().run(), "Scene break")}
      <span className="mx-1 h-5 w-px bg-stone-300" />
      <Button size="sm" variant="ghost" onClick={() => editor.chain().focus().undo().run()} disabled={!editor.can().undo()}>
        Undo
      </Button>
      <Button size="sm" variant="ghost" onClick={() => editor.chain().focus().redo().run()} disabled={!editor.can().redo()}>
        Redo
      </Button>
    </div>
  );
}

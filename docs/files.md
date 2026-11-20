# Files (second brain)

The Files page edits one folder of your own, the second brain, where agents
work. Its path is the `SECOND_BRAIN_DIR` setting (`~/second-brain` by default)
and can be changed from the page.

## Behavior

- The tree hides dot entries and `node_modules`; at most 5,000 entries load.
- Markdown notes open in a formatted editor (TipTap, vendored in
  `ui/next/vendor/editor`, rebuilt with `tools/editor/build.sh`). Other text
  files open as plain text; images are shown; other files are not opened.
- Saves happen as you type. A save sends the modification time the note had
  when it was read; if the file changed since (an agent, another app), the
  save is refused and the page offers to reload or keep your version.
- Files changed outside the editor in the last 24 hours are marked.
- Delete moves the entry to the folder's `.trash`; nothing is removed.
- Note images are read from the folder only; external image URLs are not
  loaded.

## API

All routes need an authenticated session; writes also need the usual
same-origin request headers.

| Route | Purpose |
| --- | --- |
| `GET/POST /api/files/root` | Read or choose the folder (`{path, create}`) |
| `GET /api/files/tree` | Folder tree with sizes, times and the outside-change flag |
| `GET /api/files/read?path=` | Text of a file (2 MiB at most) |
| `GET /api/files/raw?path=` | Bytes of an image; other types download as attachments |
| `POST /api/files/write` | Save `{path, content, modified, create}`; 409 on conflict |
| `POST /api/files/op` | `{op: mkdir, rename, delete, path, to}` |

Paths are relative to the folder. Absolute paths, `..`, hidden segments and
symlinks leading outside the folder are refused: every access goes through Go's
`os.Root`.

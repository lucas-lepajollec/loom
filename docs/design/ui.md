# Loom interface design

Loom Noir distinguishes surfaces with shades of gray. Session lists use
spacing and selected backgrounds to group rows. Red indicates errors or a
near limit. Diff lines use soft green and red. The light theme is still
available through Settings.

The shared styles live in [app.css](../../internal/loom/ui/next/css/app.css)
[noir.css](../../internal/loom/ui/next/css/noir.css), and
[noir-pages.css](../../internal/loom/ui/next/css/noir-pages.css).

## Tokens

| Use | Tokens |
| --- | --- |
| Workspace, panel, field | `--bg`, `--panel`, and `--bg-2` |
| Group surface, quiet control, hover | `--surface`, `--surface-2`, and `--surface-3` |
| Popover, modal, backdrop | `--raised`, `--shadow-pop`, and `--scrim` |
| Separators, field outlines, emphasis | `--line-soft`, `--line`, and `--line-strong` |
| Content, secondary text, hints, placeholders | `--text`, `--text-2`, `--text-3`, and `--text-4` |
| Primary action, focus, selected row | `--ink` with `--on-ink`, `--focus`, and `--sel` |
| Status | Neutral text for normal states in dark mode, `--red` for alerts, and `--diff-add` and `--diff-del` for changed lines |
| Spacing | `--space-1` through `--space-6` at 4, 8, 12, 16, 20, and 24 px. `--space-8` is 32 px. |
| Corners | `--r-xs` (6 px), `--r-sm` (7 px), `--r` (9 px), `--r-lg` (12 px), `--r-xl` (16 px), and `--r-pill` for rounded indicators |
| Type | `--font` (Geist) for UI text and `--mono` (Geist Mono) for code, commands, and paths |
| Type scale | `--text-xs` (11.5 px) for notes, `--text-sm` (12.5 px) for compact labels, `--text-control` (13 px), `--text-base` (13.5 px), `--text-heading` (17 px) for dialog titles, and `--text-title` (22 px) for page titles |
| Control sizes | `--control-sm` (28 px), `--control-size` (32 px), `--control-lg` (40 px), and `--touch-target` (44 px) |

## Components

| Component | Rule and implementation |
| --- | --- |
| Buttons | Use `.btn.primary` for the main action and `.btn` for secondary actions. Both have a filled surface and `border: 0`. Use `.sm` or `.lg` for size and `.danger` for a destructive action. Icon-only actions use `.icon-btn` with an accessible name. Text links use an anchor. |
| Dropdowns and selects | Use `ListPick` for a value choice. Use `DropdownButton` and `Menu` for actions or navigation. The trigger is quiet text with a trailing chevron: no fill until hovered or open. Give it the field label and let the selected text truncate if space is tight. Use `.fit` when the trigger should size to its text. |
| Menus and context menus | Use `Menu` for the shared raised surface, item rows, selected checkmark, separators, and optional groups. Use `Popover` with `.item` rows for a picker with its own search or for rich content. The trigger, an outside click, and Escape dismiss the open layer. Keyboard focus returns to the trigger on Escape or selection. |
| Segmented choices | Use `Seg` for a short set of mutually exclusive choices. The active option has one quiet pill and the other options are text. Use `TabNav` or `SectionTabs` for page navigation. |
| Modals | Use `Modal` with a title, optional subtitle, and a close button. Put content in the scrollable body and actions in `foot`. Align the footer actions to the right with secondary actions before the primary action. |
| Inputs | Use `.field` for the label and help text, `.input` for single-line fields, `.textarea` for multiline fields, and `.search` for an input with a search icon. Use native checkbox and radio inputs for form choices. |
| Switches and sliders | Use `Switch` for an on/off setting and `Slider` for a bounded numeric value. Provide an accessible label and keep the numeric value visible when it matters. |
| Logos and icons | Use `Logo` for vendors and models, `CliMark` for agents, and `Icon` for UI symbols. Use `.logo-mark` to center a standalone symbol or fallback initials on a transparent surface. Use `.xs`, `.sm`, or `.lg` to size the mark to its context. |

The component sources are [controls.js](../../internal/loom/ui/next/js/ui/controls.js),
[listpick.js](../../internal/loom/ui/next/js/ui/listpick.js),
[dialog.js](../../internal/loom/ui/next/js/ui/dialog.js),
[tabnav.js](../../internal/loom/ui/next/js/ui/tabnav.js), and
[logo.js](../../internal/loom/ui/next/js/ui/logo.js).

On desktop the 56 px rail uses 40 px buttons and 18 px icons. The 264 px
context column shows sessions, files, machines, alerts or page links, with
36 px rows. The 430 px session panel overlays the workspace below 1180 px.
Below 860 px the context column gives way to the in-page navigation.

Below 640 px the bottom tabs are Sessions, Files, Inbox, and More. The
session thread, file viewer, and new-session form use the full screen.
Session details and agent approvals open as bottom sheets. The composer
stays above the safe area or keyboard. The route crawl checks 390 px and
320 px layouts with deterministic data and long labels.
[Phone layout checks](../../tools/tests/ui-routes/layout.mjs)

The local Model panel uses two fine sliders on Essential. Context includes
a numeric input and shows the native and effective values. Advanced groups
sampling, memory and speed, and speculative decoding. Expert keeps the
filterable native flags. Both tiers use fields, menus, and switches.

The desktop Files column keeps the tree, source selector, search, and recent
files next to the editor. On a phone the tree and editor have separate
screens. The Models column groups the library, Hub, Cloud, Voice, and Engine.
Agents gives Tools and History their own pages alongside Overview, Scheduled,
and the Codeman connection. Settings links directly to account access, agent
access, policy, notifications, updates, network, and data controls.

Inbox uses a selected item beside its desktop list and cards on phones.
Mark all read clears completed-session notices. Approvals and reply requests
stay available until handled. The command palette searches session names,
model names, file names in the configured Files tree, and messages already
displayed in the active thread. It opens links in a new tab with Ctrl/⌘+Enter.

Activity and Machines keep their existing charts, cards, and content.

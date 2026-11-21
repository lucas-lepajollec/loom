# Loom interface design

Use quiet surfaces, a clear type hierarchy, and consistent controls across
light and dark themes. Group related settings in rows on a single surface.
Use spacing and a subtle separator to show where one setting ends and the
next begins. Page widths and top spacing come from the layout stylesheet.

The shared styles live in [app.css](../../internal/loom/ui/next/css/app.css).

## Tokens

| Use | Tokens |
| --- | --- |
| Workspace, panel, field | `--bg`, `--panel`, and `--bg-2` |
| Group surface, quiet control, hover | `--surface`, `--surface-2`, and `--surface-3` |
| Popover, modal, backdrop | `--raised`, `--shadow-pop`, and `--scrim` |
| Separators, field outlines, emphasis | `--line-soft`, `--line`, and `--line-strong` |
| Content, secondary text, hints, placeholders | `--text`, `--text-2`, `--text-3`, and `--text-4` |
| Primary action, focus, selected row | `--ink` with `--on-ink`, `--focus`, and `--sel` |
| Status | `--green` for ready or successful, `--amber` for pending or attention, `--red` for errors and destructive actions |
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

On phones, use 44 px touch targets and allow control groups and dialog
actions to wrap. Check at 390 px and 320 px, including long labels and open
menus. Use the viewport-aware positioning in `Popover` and keep enough
room for the dialog body to scroll while the header and footer stay visible.
The route crawl exercises these layouts with deterministic data.
[Phone layout checks](../../tools/tests/ui-routes/layout.mjs)

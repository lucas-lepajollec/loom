# Loom interface design

Use quiet surfaces, a clear type hierarchy, and consistent controls across
light and dark themes. Group related settings in rows on a single surface.
Use spacing and a subtle separator to show where one setting ends and the
next begins. Page widths and top spacing come from the layout stylesheet.

The shared styles live in [app.css](../../internal/loom/ui/next/css/app.css).

## Workspace navigation

The desktop rail opens the main product areas. The Loom mark toggles the session library. The tabs above the current page return to recently visited areas and individual agent sessions in this window. Closing a tab leaves the running session alone. Search opens matching agent sessions and saved machines. Connections presents the Loom MCP gateway and links to Codeman, cloud providers and policy. On phones the bottom navigation remains the primary navigation.

Agent sessions expose conversation, terminal and review tools in one toolbar. Changes, files and machine-selected application previews open beside the session on desktop and in sheets on phones. Select a line in a unified diff and choose **Add line to reply** to prepare an editable draft in a compatible conversation. Deleted lines use the original file’s line number.

The dark and light surface palettes and session workspace are inspired by [Paradev](https://github.com/ParadevAI/Paradev/tree/cc4f7947da109f6136db9339270d416b10693820). Copper signals selection. Working-state motion follows the system reduced-motion preference. [MIT notice](licenses/paradev.txt).

The command palette adapts Paradev’s command scoring and match highlighting. It indexes settings and visible headings on the current page. Desktop tool panels support dragging, arrow keys, Home, End and double-click to reset. The resize control adapts Paradev’s PanelResizeHandle. Both ports use the notices in [the MIT attribution](licenses/paradev.txt). Settings → About also opens the notice served by the local binary.

Inbox filters approvals, replies and completions, with a separate live engine jobs view. Transcript search locates loaded messages in the current discussion or agent session. In Files, the document outline jumps to headings in the rendered note.

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

At widths up to 760 px, use Sessions, Inbox, Usage, and More as the bottom
tabs. Keep the composer just above the safe area or keyboard. Session headers
provide a back button and a short title or model selector. Use 44 px touch
targets, 16 px text fields, and wrap control groups when space is tight.
Shared dialogs and menus open as bottom sheets. Leave the header and actions
within reach when the sheet body scrolls. The route crawl checks 390 px and
320 px layouts with deterministic data and long labels.
[Phone layout checks](../../tools/tests/ui-routes/layout.mjs)

From More, Models and Agents use wrapping section chips. Models groups the
engine, presets, and library, and puts row actions in overflow sheets.
Agents leads with Codeman status, harnesses, and upcoming runs. Machine cards
open a detail page with terminal access and resource meters. Settings offers
quick preferences and a notification switch for the current device, with
links to the full settings pages. Use a named back link to the parent page
throughout these views.

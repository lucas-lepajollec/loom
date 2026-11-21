"use strict";
var XtermZerolagInput = (() => {
  var __defProp = Object.defineProperty;
  var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
  var __getOwnPropNames = Object.getOwnPropertyNames;
  var __hasOwnProp = Object.prototype.hasOwnProperty;
  var __export = (target, all) => {
    for (var name in all)
      __defProp(target, name, { get: all[name], enumerable: true });
  };
  var __copyProps = (to, from, except, desc) => {
    if (from && typeof from === "object" || typeof from === "function") {
      for (let key of __getOwnPropNames(from))
        if (!__hasOwnProp.call(to, key) && key !== except)
          __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
    }
    return to;
  };
  var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);

  // src/index.ts
  var index_exports = {};
  __export(index_exports, {
    PredictiveEchoAddon: () => PredictiveEchoAddon,
    ZerolagInputAddon: () => ZerolagInputAddon,
    charCellWidth: () => charCellWidth,
    stringCellWidth: () => stringCellWidth
  });

  // src/cell-dimensions.ts
  function getCellDimensions(terminal) {
    const t = terminal;
    const dpr = typeof devicePixelRatio === "number" && devicePixelRatio > 0 ? devicePixelRatio : 1;
    if (t.dimensions?.css?.cell) {
      const cellH = t.dimensions.css.cell.height;
      return {
        width: t.dimensions.css.cell.width,
        height: cellH,
        charTop: (t.dimensions?.device?.char?.top ?? 0) / dpr,
        charHeight: (t.dimensions?.device?.char?.height ?? cellH * dpr) / dpr
      };
    }
    try {
      const dims = t._core?._renderService?.dimensions;
      if (dims?.css?.cell) {
        const cellH = dims.css.cell.height;
        return {
          width: dims.css.cell.width,
          height: cellH,
          charTop: (dims.device?.char?.top ?? 0) / dpr,
          charHeight: (dims.device?.char?.height ?? cellH * dpr) / dpr
        };
      }
    } catch {
    }
    return null;
  }

  // src/prompt-finder.ts
  function findPrompt(terminal, finder) {
    try {
      const buffer = terminal.buffer.active;
      const viewportTop = buffer.viewportY;
      switch (finder.type) {
        case "character": {
          for (let row = terminal.rows - 1; row >= 0; row--) {
            const line = buffer.getLine(viewportTop + row);
            if (!line) continue;
            const text = line.translateToString(true);
            const idx = text.lastIndexOf(finder.char);
            if (idx >= 0) return { row, col: idx };
          }
          return null;
        }
        case "regex": {
          const pattern = finder.pattern;
          const safePattern = pattern.global ? new RegExp(pattern.source, pattern.flags.replace("g", "")) : pattern;
          for (let row = terminal.rows - 1; row >= 0; row--) {
            const line = buffer.getLine(viewportTop + row);
            if (!line) continue;
            const text = line.translateToString(true);
            const match = text.match(safePattern);
            if (match) {
              const col = match.index ?? 0;
              return { row, col };
            }
          }
          return null;
        }
        case "custom":
          return finder.find(terminal);
        default:
          return null;
      }
    } catch {
      return null;
    }
  }
  function readTextAfterPrompt(terminal, prompt, offset) {
    try {
      const buffer = terminal.buffer.active;
      const absRow = buffer.viewportY + prompt.row;
      const line = buffer.getLine(absRow);
      if (!line) return "";
      const lineText = line.translateToString(true);
      return lineText.slice(prompt.col + offset).trimEnd();
    } catch {
      return "";
    }
  }
  function promptRowInViewport(terminal) {
    try {
      const buf = terminal.buffer.active;
      if (buf.viewportY === buf.baseY) return true;
      if (typeof buf.cursorY !== "number") return false;
      const cursorRow = buf.baseY + buf.cursorY;
      return cursorRow >= buf.viewportY && cursorRow < buf.viewportY + terminal.rows;
    } catch {
      return false;
    }
  }

  // src/overlay-renderer.ts
  function charCellWidth(terminal, ch) {
    if (terminal?.unicode?.getStringCellWidth) {
      return terminal.unicode.getStringCellWidth(ch);
    }
    const code = ch.codePointAt(0);
    if (code !== void 0 && code >= 4352 && (code <= 4447 || // Hangul Jamo
    code >= 11904 && code <= 12350 || // CJK Radicals, Kangxi, Ideographic
    code >= 12352 && code <= 13247 || // Hiragana, Katakana, Bopomofo, CJK Compat
    code >= 13312 && code <= 19903 || // CJK Unified Ext A
    code >= 19968 && code <= 42191 || // CJK Unified, Yi
    code >= 43360 && code <= 43388 || // Hangul Jamo Extended-A
    code >= 44032 && code <= 55203 || // Hangul Syllables
    code >= 63744 && code <= 64255 || // CJK Compat Ideographs
    code >= 65072 && code <= 65135 || // CJK Compat Forms
    code >= 65281 && code <= 65376 || // Fullwidth Forms
    code >= 65504 && code <= 65510 || // Fullwidth Signs
    code >= 126976 && code <= 130047 || // Mahjong, Domino, Emoji
    code >= 131072 && code <= 196607 || // CJK Unified Ext B-F
    code >= 196608 && code <= 262143))
      return 2;
    return 1;
  }
  function stringCellWidth(terminal, str) {
    let w = 0;
    for (const ch of str) w += charCellWidth(terminal, ch);
    return w;
  }
  var EDGE_SNAP = 4;
  function cellEdge(col, cellW) {
    return Math.round(col * cellW * EDGE_SNAP) / EDGE_SNAP;
  }
  var rendered = /* @__PURE__ */ new WeakMap();
  function holdsOnlyRendered(container, state) {
    const children = container.children;
    if (children.length !== state.lines.length + (state.cursor ? 1 : 0)) return false;
    for (let i = 0; i < state.lines.length; i++) {
      if (children[i] !== state.lines[i]) return false;
    }
    return !state.cursor || children[state.lines.length] === state.cursor;
  }
  function renderOverlay(container, params) {
    const {
      lines,
      compositionStart,
      startCol,
      totalCols,
      cellW,
      cellH,
      charTop,
      charHeight,
      promptRow,
      totalRows,
      font,
      showCursor,
      cursorColor,
      terminal
    } = params;
    const rows = totalRows && totalRows > 0 ? totalRows : terminal?.rows;
    const lineOffsets = [];
    const lineLengths = [];
    {
      let offset = 0;
      for (const line of lines) {
        const length = [...line].length;
        lineOffsets.push(offset);
        lineLengths.push(length);
        offset += length;
      }
    }
    let visibleLines = lines;
    let firstVisible = 0;
    let keepsPromptLine = true;
    let topRow = promptRow;
    if (rows && rows > 0) {
      if (lines.length > rows) {
        firstVisible = lines.length - rows;
        visibleLines = lines.slice(firstVisible);
        keepsPromptLine = false;
        topRow = 0;
      } else if (promptRow + lines.length > rows) {
        topRow = rows - lines.length;
      }
    }
    topRow = Math.max(0, topRow);
    container.style.left = "0px";
    container.style.top = topRow * cellH + "px";
    const styleKey = [
      cellW,
      cellH,
      font.fontFamily,
      font.fontSize,
      font.fontWeight,
      font.color,
      font.backgroundColor,
      font.letterSpacing,
      terminal?.unicode?.activeVersion ?? ""
    ].join("\n");
    let state = rendered.get(container);
    if (!state || state.styleKey !== styleKey || !holdsOnlyRendered(container, state)) {
      container.innerHTML = "";
      state = { styleKey, lines: [], lineKeys: [], cursor: null };
      rendered.set(container, state);
    }
    state.cursor?.remove();
    state.cursor = null;
    const fullWidthPx = totalCols * cellW;
    for (let i = 0; i < visibleLines.length; i++) {
      const indents = i === 0 && keepsPromptLine;
      const leftPx = indents ? startCol * cellW : 0;
      const widthPx = indents ? fullWidthPx - leftPx : fullWidthPx;
      const topPx = i * cellH;
      let lineCompositionFrom;
      if (compositionStart !== void 0) {
        const from = compositionStart - lineOffsets[firstVisible + i];
        if (from < lineLengths[firstVisible + i]) lineCompositionFrom = Math.max(0, from);
      }
      const key = `${leftPx}|${topPx}|${widthPx}|${lineCompositionFrom ?? ""}|${visibleLines[i]}`;
      if (state.lineKeys[i] === key) continue;
      const lineEl = makeLine(
        visibleLines[i],
        leftPx,
        topPx,
        widthPx,
        cellH,
        cellW,
        charTop,
        charHeight,
        font,
        terminal,
        lineCompositionFrom
      );
      const previous = state.lines[i];
      if (previous) container.replaceChild(lineEl, previous);
      else container.appendChild(lineEl);
      state.lines[i] = lineEl;
      state.lineKeys[i] = key;
    }
    for (let i = state.lines.length - 1; i >= visibleLines.length; i--) state.lines[i].remove();
    state.lines.length = visibleLines.length;
    state.lineKeys.length = visibleLines.length;
    if (showCursor) {
      const lastLine = visibleLines[visibleLines.length - 1];
      const lastLineLeft = visibleLines.length === 1 && keepsPromptLine ? startCol : 0;
      const cursorCol = lastLineLeft + stringCellWidth(terminal, lastLine);
      if (cursorCol < totalCols) {
        const cursor = document.createElement("span");
        cursor.style.cssText = "position:absolute;display:inline-block";
        cursor.style.left = cursorCol * cellW + "px";
        cursor.style.top = (visibleLines.length - 1) * cellH + "px";
        cursor.style.width = cellW + "px";
        cursor.style.height = cellH + "px";
        cursor.style.backgroundColor = cursorColor;
        container.appendChild(cursor);
        state.cursor = cursor;
      }
    }
    container.style.display = "";
  }
  function makeLine(text, leftPx, topPx, widthPx, cellH, cellW, _charTop, _charHeight, font, terminal, compositionFrom) {
    const el = document.createElement("div");
    el.style.cssText = "position:absolute;display:flex;pointer-events:none;text-align:center;font-feature-settings:'liga' 0,'calt' 0";
    el.style.backgroundColor = font.backgroundColor;
    el.style.left = leftPx + "px";
    el.style.top = topPx + "px";
    el.style.width = widthPx + "px";
    el.style.height = cellH + 1 + "px";
    el.style.lineHeight = cellH + "px";
    el.style.fontFamily = font.fontFamily;
    el.style.fontSize = font.fontSize;
    el.style.fontWeight = font.fontWeight;
    el.style.color = font.color;
    if (font.letterSpacing) el.style.letterSpacing = font.letterSpacing;
    const spanHeight = ";height:" + cellH + "px";
    let col = 0;
    let edge = 0;
    let index = 0;
    for (const ch of text) {
      col += charCellWidth(terminal, ch);
      const nextEdge = cellEdge(col, cellW);
      const span = document.createElement("span");
      span.style.cssText = "flex:none;width:" + (nextEdge - edge) + "px" + spanHeight;
      if (compositionFrom !== void 0 && index >= compositionFrom) {
        span.style.textDecoration = "underline";
        span.setAttribute("data-zerolag-composition", "");
        span.setAttribute("aria-hidden", "true");
      }
      span.textContent = ch;
      el.appendChild(span);
      edge = nextEdge;
      index++;
    }
    return el;
  }

  // src/zerolag-input-addon.ts
  var DEFAULT_PROMPT = { type: "character", char: ">", offset: 2 };
  var DEFAULT_Z_INDEX = 7;
  var DEFAULT_SCROLL_DEBOUNCE_MS = 50;
  var DEFAULT_BG = "#0d0d0d";
  var DEFAULT_FG = "#eeeeee";
  var DEFAULT_CURSOR = "#e0e0e0";
  var ZerolagInputAddon = class {
    _terminal = null;
    _overlay = null;
    _options;
    // Text state
    _pendingText = "";
    _flushedOffset = 0;
    _flushedText = "";
    _bufferDetectDone = false;
    // IME text still being composed: drawn after the pending text, never sent.
    _composition = "";
    // Rows from the top of the screen that are actually visible (see setRowLimit);
    // null means all of them.
    _rowLimit = null;
    // Render cache
    _lastRenderKey = "";
    _lastPromptPos = null;
    // Font cache
    _font = {
      fontFamily: "monospace",
      fontSize: "14px",
      fontWeight: "normal",
      color: DEFAULT_FG,
      backgroundColor: DEFAULT_BG,
      letterSpacing: ""
    };
    // Scroll handling
    _scrollTimer = null;
    _scrollHandler = null;
    _scrollViewport = null;
    constructor(options) {
      this._options = {
        prompt: options?.prompt ?? DEFAULT_PROMPT,
        zIndex: options?.zIndex ?? DEFAULT_Z_INDEX,
        showCursor: options?.showCursor ?? true,
        scrollDebounceMs: options?.scrollDebounceMs ?? DEFAULT_SCROLL_DEBOUNCE_MS,
        backgroundColor: options?.backgroundColor,
        foregroundColor: options?.foregroundColor,
        cursorColor: options?.cursorColor
      };
    }
    // ─── Lifecycle ────────────────────────────────────────────────────
    /**
     * Called by `terminal.loadAddon()`. Do not call directly.
     */
    activate(terminal) {
      this._terminal = terminal;
      this._overlay = document.createElement("div");
      this._overlay.style.cssText = `position:absolute;z-index:${this._options.zIndex};pointer-events:none;display:none`;
      const screen = terminal.element?.querySelector(".xterm-screen");
      if (screen) {
        screen.appendChild(this._overlay);
      }
      this._cacheFont();
      this._scrollHandler = () => {
        try {
          if (!promptRowInViewport(this._terminal)) {
            this._overlay.style.display = "none";
            if (this._scrollTimer) {
              clearTimeout(this._scrollTimer);
              this._scrollTimer = null;
            }
          } else if (this._hasContent()) {
            if (this._scrollTimer) clearTimeout(this._scrollTimer);
            this._scrollTimer = setTimeout(() => {
              this._scrollTimer = null;
              this._lastRenderKey = "";
              this._render();
            }, this._options.scrollDebounceMs);
          }
        } catch {
        }
      };
      const viewport = terminal.element?.querySelector(".xterm-viewport");
      if (viewport) {
        viewport.addEventListener("scroll", this._scrollHandler, { passive: true });
        this._scrollViewport = viewport;
      }
    }
    /**
     * Remove the overlay, clean up listeners.
     */
    dispose() {
      this.clear();
      if (this._scrollTimer) {
        clearTimeout(this._scrollTimer);
        this._scrollTimer = null;
      }
      if (this._scrollViewport && this._scrollHandler) {
        this._scrollViewport.removeEventListener("scroll", this._scrollHandler);
      }
      this._overlay?.remove();
      this._overlay = null;
      this._scrollViewport = null;
      this._scrollHandler = null;
      this._terminal = null;
    }
    // ─── Input methods ────────────────────────────────────────────────
    /**
     * Add a single printable character to the overlay.
     * Call this when the user types a character (charCode >= 32, length === 1).
     */
    addChar(char) {
      if (!this._pendingText && !this._flushedOffset) this._detectBufferText();
      this._pendingText += char;
      this._render();
    }
    /**
     * Append multiple characters at once (e.g., paste).
     */
    appendText(text) {
      if (!text) return;
      if (!this._pendingText && !this._flushedOffset) this._detectBufferText();
      this._pendingText += text;
      this._render();
    }
    /**
     * Remove the last character from the overlay.
     *
     * Cascade order:
     * 1. Remove from `pendingText` if non-empty → returns `'pending'`
     * 2. Decrement `flushedOffset` if pending is empty but flushed exists → returns `'flushed'`
     * 3. Try `detectBufferText()` if both are empty, then decrement → returns `'flushed'`
     *
     * @returns The source of the removed character, or `false` if nothing to remove.
     *
     * - `'pending'`: A character was removed from unsent text. The consumer
     *   should NOT send backspace to the PTY (the text was never transmitted).
     * - `'flushed'`: A character was removed from text already sent to the PTY.
     *   The consumer SHOULD send backspace to the PTY.
     * - `false`: Nothing to remove. The consumer should NOT send backspace.
     *
     * Any IME composition is dropped in every case, and the overlay is repainted
     * without it (hidden when nothing else is left).
     */
    removeChar() {
      const droppedComposition = this._composition.length > 0;
      this._composition = "";
      if (this._pendingText.length > 0) {
        this._pendingText = this._pendingText.slice(0, -1);
        if (this._pendingText.length > 0 || this._flushedOffset > 0) {
          this._render();
        } else {
          this._hide();
        }
        return "pending";
      }
      if (this._flushedOffset > 0) {
        this._flushedOffset--;
        this._flushedText = this._flushedText.slice(0, -1);
        if (this._flushedOffset > 0) {
          this._render();
        } else {
          this._hide();
        }
        return "flushed";
      }
      this._detectBufferText();
      if (this._flushedOffset > 0) {
        this._flushedOffset--;
        this._flushedText = this._flushedText.slice(0, -1);
        if (this._flushedOffset > 0) {
          this._render();
        } else {
          this._hide();
        }
        return "flushed";
      }
      if (droppedComposition) this._hide();
      return false;
    }
    /**
     * Clear all overlay state (pending + flushed). Hides the overlay.
     * Call on Enter, Ctrl+C, or any action that submits/cancels input.
     */
    clear() {
      this._pendingText = "";
      this._composition = "";
      this._flushedOffset = 0;
      this._flushedText = "";
      this._bufferDetectDone = false;
      this._lastRenderKey = "";
      this._lastPromptPos = null;
      this._hide();
    }
    // ─── Flushed text tracking ────────────────────────────────────────
    /**
     * Mark characters as "flushed" — sent to PTY but echo not yet received.
     *
     * The overlay renders flushed text (from the stored string) with an opaque
     * background to cover the terminal's canvas text, preventing a visible
     * font mismatch between canvas and DOM rendering.
     *
     * @param count - Number of characters flushed
     * @param text - The actual flushed text (avoids reading stale terminal buffer)
     * @param render - Whether to re-render immediately (default: `true`).
     *   Pass `false` when restoring flushed state during a tab/session switch
     *   before the new buffer has loaded — rendering against a stale buffer
     *   would lock the prompt column to the wrong position. Call `rerender()`
     *   explicitly after the buffer finishes loading.
     */
    setFlushed(count, text, render = true) {
      this._flushedOffset = count;
      this._flushedText = text;
      if (render) this._render();
    }
    /**
     * Get current flushed state.
     */
    getFlushed() {
      return { count: this._flushedOffset, text: this._flushedText };
    }
    /**
     * Clear flushed state. Call when server echo has arrived and the terminal
     * buffer now contains the flushed text.
     */
    clearFlushed() {
      this._flushedOffset = 0;
      this._flushedText = "";
      if (this._pendingText || this._composition) {
        this._render();
      } else {
        this._hide();
      }
    }
    // ─── Rendering control ────────────────────────────────────────────
    /**
     * Force a re-render of the overlay at the current prompt position.
     * Call after terminal resets, buffer reloads, or full-screen redraws
     * that move the prompt.
     */
    rerender() {
      if (this._hasContent()) {
        this._lastRenderKey = "";
        this._render();
      }
    }
    /**
     * Bound the layout to the first `limit` rows of the screen, or to all of them
     * with `null`. It narrows the bound the overlay already keeps itself on
     * screen with (`totalRows`, see _layoutRows), never adds a second one.
     *
     * For a host that keeps the terminal taller than the box showing it and
     * scrolls the screen element instead (e.g. holding the PTY's rows while an
     * on-screen keyboard covers the bottom): the composer must grow upward from
     * the last row the user can SEE, not from the terminal's last row, which is
     * under the keyboard.
     */
    setRowLimit(limit) {
      const next = limit !== null && Number.isFinite(limit) && limit > 0 ? Math.trunc(limit) : null;
      if (next === this._rowLimit) return;
      this._rowLimit = next;
      this.rerender();
    }
    /**
     * The `totalRows` bound handed to renderOverlay: the one place that decides
     * where the block stops growing downward. The terminal's rows, narrowed by
     * setRowLimit when the host shows fewer; a limit above the terminal's rows
     * changes nothing.
     */
    _layoutRows() {
      const rows = this._terminal.rows;
      return this._rowLimit !== null ? Math.min(this._rowLimit, rows) : rows;
    }
    /**
     * Re-read font properties from the terminal and re-render.
     * Call after font size changes, theme changes, etc.
     */
    refreshFont() {
      this._cacheFont();
      this._lastRenderKey = "";
      if (this._hasContent()) this._render();
    }
    // ─── Buffer detection ─────────────────────────────────────────────
    /**
     * Scan the terminal buffer for text after the prompt marker.
     * If found, sets it as flushed text in the overlay.
     *
     * Use case: Tab completion filled text on the prompt that the overlay
     * doesn't know about. Call this to sync overlay state with the buffer.
     *
     * @returns The detected text, or `null` if no prompt or no text found.
     */
    detectBufferText() {
      return this._detectBufferText();
    }
    /**
     * Reset the buffer detection guard. After `clear()`, detection is
     * automatically re-enabled. Call this manually if you need to force
     * re-detection (e.g., after a tab completion response arrives).
     */
    resetBufferDetection() {
      this._bufferDetectDone = false;
    }
    /**
     * Undo the last `detectBufferText()` call — clears flushed state and
     * re-enables detection.
     *
     * Use case: Tab completion detection found text that matches the
     * pre-tab baseline (no real completion happened). Call this to undo
     * the detection so it can retry on the next flush cycle.
     */
    undoDetection() {
      this._flushedOffset = 0;
      this._flushedText = "";
      this._bufferDetectDone = false;
    }
    /**
     * Suppress buffer detection until the next `clear()` or
     * `resetBufferDetection()` call.
     *
     * Use case: When switching to a session whose buffer contains UI
     * framework text (e.g., Ink status bars) after the prompt marker,
     * `detectBufferText()` would falsely pick up that text as user input.
     * Call this after switching to prevent false detection until the user
     * actually presses Enter (which calls `clear()` and re-enables detection).
     */
    suppressBufferDetection() {
      this._bufferDetectDone = true;
    }
    // ─── Prompt configuration ──────────────────────────────────────────
    /**
     * Change the prompt detection strategy at runtime.
     * Call this when switching between CLI modes (e.g., Claude Code vs OpenCode)
     * that use different prompt characters.
     */
    setPrompt(finder) {
      this._options.prompt = finder;
      this._lastPromptPos = null;
      this._lastRenderKey = "";
      if (this._hasContent()) this._render();
    }
    // ─── IME composition ──────────────────────────────────────────────
    /**
     * Show text an IME is still composing as an underlined tail after the
     * pending text, wrapped and kept on screen like the rest of the overlay.
     * Pass `''` to remove it.
     *
     * Visual only: the composition is never part of `pendingText`, `hasPending`
     * or anything a consumer sends. When the IME commits, the consumer adds the
     * committed text the usual way (`addChar`/`appendText`) and clears the
     * composition. `clear()` and `removeChar()` drop it too.
     */
    setComposition(text) {
      const next = typeof text === "string" ? text.replace(/[\u0000-\u001f\u007f-\u009f\u2028\u2029]/g, "") : "";
      if (next === this._composition) return;
      this._composition = next;
      if (this._hasContent()) {
        this._render();
      } else {
        this._hide();
      }
    }
    /** Text an IME is still composing, drawn after `pendingText` (never sent). */
    get composition() {
      return this._composition;
    }
    // ─── Prompt utilities ─────────────────────────────────────────────
    /**
     * Find the prompt in the terminal buffer using the configured strategy.
     * @returns The position or `null` if not found.
     */
    findPrompt() {
      if (!this._terminal) return null;
      return findPrompt(this._terminal, this._options.prompt ?? DEFAULT_PROMPT);
    }
    /**
     * Read text after the prompt marker on the prompt line.
     * Convenience method for consumers that need to snapshot prompt content.
     */
    readPromptText() {
      if (!this._terminal) return null;
      const prompt = this.findPrompt();
      if (!prompt) return null;
      const offset = this._getPromptOffset();
      const text = readTextAfterPrompt(this._terminal, prompt, offset);
      return text || null;
    }
    // ─── Public state ─────────────────────────────────────────────────
    /** Current pending (unacknowledged) text. */
    get pendingText() {
      return this._pendingText;
    }
    /**
     * Whether there is pending or flushed text. Excludes the IME composition,
     * which is never sent, so an overlay showing only a composition reports
     * `false` while still on screen. To re-place the overlay after output or a
     * resize, call `rerender()` unconditionally: it is a no-op when there is
     * nothing to draw.
     */
    get hasPending() {
      return this._pendingText.length > 0 || this._flushedOffset > 0;
    }
    /** Read-only state snapshot. */
    get state() {
      return {
        pendingText: this._pendingText,
        flushedLength: this._flushedOffset,
        flushedText: this._flushedText,
        visible: this._overlay !== null && this._overlay.style.display !== "none",
        promptPosition: this._lastPromptPos ? { ...this._lastPromptPos } : null
      };
    }
    // ─── Private methods ──────────────────────────────────────────────
    _hasContent() {
      return this._pendingText.length > 0 || this._flushedOffset > 0 || this._composition.length > 0;
    }
    _getPromptOffset() {
      const prompt = this._options.prompt ?? DEFAULT_PROMPT;
      return prompt.offset ?? 2;
    }
    _detectBufferText() {
      if (this._bufferDetectDone) return null;
      if (!this._terminal) return null;
      try {
        const prompt = this.findPrompt();
        if (!prompt) return null;
        const offset = this._getPromptOffset();
        const afterPrompt = readTextAfterPrompt(this._terminal, prompt, offset);
        if (afterPrompt.length > 0) {
          this._flushedOffset = afterPrompt.length;
          this._flushedText = afterPrompt;
          this._lastPromptPos = prompt;
          this._bufferDetectDone = true;
          return afterPrompt;
        }
      } catch {
      }
      return null;
    }
    _cacheFont() {
      if (!this._terminal) return;
      const t = this._terminal;
      this._font.fontFamily = t.options.fontFamily || "monospace";
      this._font.fontSize = (t.options.fontSize || 14) + "px";
      this._font.fontWeight = String(t.options.fontWeight || "normal");
      this._font.backgroundColor = this._options.backgroundColor ?? t.options.theme?.background ?? DEFAULT_BG;
      this._font.color = this._options.foregroundColor ?? t.options.theme?.foreground ?? DEFAULT_FG;
      this._font.letterSpacing = "";
      const rows = t.element?.querySelector(".xterm-rows");
      if (rows) {
        const cs = getComputedStyle(rows);
        this._font.letterSpacing = cs.letterSpacing;
        if (!this._options.foregroundColor && cs.color) {
          this._font.color = cs.color;
        }
      }
    }
    _hide() {
      if (!this._overlay) return;
      this._lastRenderKey = "";
      this._lastPromptPos = null;
      this._overlay.innerHTML = "";
      this._overlay.style.display = "none";
    }
    _render() {
      if (!this._terminal || !this._overlay) return;
      if (!this._hasContent()) {
        this._overlay.style.display = "none";
        return;
      }
      try {
        const buf = this._terminal.buffer.active;
        if (!promptRowInViewport(this._terminal)) {
          this._overlay.style.display = "none";
          return;
        }
        const prompt = this.findPrompt();
        if (prompt) {
          if (this._lastPromptPos && this._flushedOffset > 0) {
            this._lastPromptPos = { row: prompt.row, col: this._lastPromptPos.col };
          } else {
            this._lastPromptPos = prompt;
          }
        } else if (!this._lastPromptPos) {
          this._overlay.style.display = "none";
          return;
        }
        const activePrompt = this._lastPromptPos;
        const dims = getCellDimensions(this._terminal);
        if (!dims) {
          this._overlay.style.display = "none";
          return;
        }
        const { width: cellW, height: cellH, charTop, charHeight } = dims;
        const totalCols = this._terminal.cols;
        const offset = this._getPromptOffset();
        const startCol = activePrompt.col + offset;
        let displayText = this._pendingText;
        if (this._flushedOffset > 0) {
          if (this._flushedText && this._flushedText.length === this._flushedOffset) {
            displayText = this._flushedText + this._pendingText;
          } else {
            const absRow = buf.viewportY + activePrompt.row;
            const line = buf.getLine(absRow);
            if (line) {
              const lineText = line.translateToString(true);
              const flushedChars = lineText.slice(startCol, startCol + this._flushedOffset);
              displayText = flushedChars + this._pendingText;
            }
          }
        }
        const compositionStart = [...displayText].length;
        displayText += this._composition;
        const totalRows = this._layoutRows();
        const renderKey = `${displayText}:${compositionStart}:${startCol}:${activePrompt.row}:${activePrompt.col}:${totalCols}:${totalRows}:${this._flushedOffset}`;
        if (renderKey === this._lastRenderKey && this._overlay.style.display !== "none") return;
        this._lastRenderKey = renderKey;
        const firstLineCols = Math.max(1, totalCols - startCol);
        const chars = [...displayText];
        const lines = [];
        let ci = 0;
        {
          let lineStr = "";
          let lineCols = 0;
          while (ci < chars.length) {
            const cw = charCellWidth(this._terminal, chars[ci]);
            if (lineCols + cw > firstLineCols) break;
            lineStr += chars[ci];
            lineCols += cw;
            ci++;
          }
          lines.push(lineStr);
        }
        while (ci < chars.length) {
          let lineStr = "";
          let lineCols = 0;
          while (ci < chars.length) {
            const cw = charCellWidth(this._terminal, chars[ci]);
            if (lineCols + cw > totalCols) break;
            lineStr += chars[ci];
            lineCols += cw;
            ci++;
          }
          lines.push(lineStr);
        }
        const cursorColor = this._options.cursorColor ?? this._terminal.options.theme?.cursor ?? DEFAULT_CURSOR;
        renderOverlay(this._overlay, {
          lines,
          compositionStart: this._composition ? compositionStart : void 0,
          startCol,
          totalCols,
          cellW,
          cellH,
          charTop,
          charHeight,
          promptRow: activePrompt.row,
          totalRows,
          font: this._font,
          showCursor: this._options.showCursor,
          cursorColor,
          terminal: this._terminal
        });
      } catch {
        if (this._overlay) {
          this._overlay.innerHTML = "";
          this._overlay.style.display = "none";
        }
      }
    }
  };

  // src/prediction-renderer.ts
  function addPredictionSpan(container, map, p) {
    const span = document.createElement("span");
    span.style.cssText = `position:absolute;left:${p.col * p.dims.width}px;top:${p.row * p.dims.height}px;width:${p.width * p.dims.width}px;height:${p.dims.height + 1}px;line-height:${p.dims.height}px;text-align:center;pointer-events:none;font-family:${p.font.fontFamily};font-size:${p.font.fontSize};font-weight:${p.font.fontWeight};` + (p.font.letterSpacing ? `letter-spacing:${p.font.letterSpacing};` : "") + `color:${p.font.color};background-color:${p.font.backgroundColor};font-feature-settings:'liga' 0,'calt' 0;` + (p.underline ? "text-decoration:underline;" : "");
    span.textContent = p.char;
    map.set(p.seq, span);
    container.appendChild(span);
  }
  function removePredictionSpan(map, seq) {
    const span = map.get(seq);
    if (span) {
      span.remove();
      map.delete(seq);
    }
  }
  function clearAllSpans(map) {
    for (const span of map.values()) span.remove();
    map.clear();
  }

  // src/predictive-echo-addon.ts
  var DEFAULT_OPTIONS = {
    zIndex: 7,
    underlinePredictions: false,
    ttlMs: 1e3,
    maxPending: 32,
    cursorGraceMs: 150,
    edgeMarginCells: 4
  };
  var DEFAULT_BG2 = "#000000";
  var DEFAULT_FG2 = "#ffffff";
  var PredictiveEchoAddon = class {
    _terminal = null;
    _container = null;
    _spans = /* @__PURE__ */ new Map();
    _outstanding = [];
    _anchor = null;
    _cursorOffRowSince = null;
    _seq = 0;
    _confirmedTotal = 0;
    _droppedTotal = 0;
    _ttlTimer = null;
    /** Anchor hold: set after an unpredicted wire edit (backspace into echoed
     *  text, any cleared input, an IME text commit). While held, new
     *  predictions are suppressed: the displayed cursor is stale until the
     *  next parsed write, and anchoring on it paints ghosts one cell off
     *  (found by review: backspace-then-retype within RTT). Cleared by the
     *  onWriteParsed pass and by public reconcile(), never by the inline
     *  predictChar pass (which runs before the display could catch up). */
    _anchorHold = false;
    _reconcileScheduled = false;
    _disposables = [];
    _predictWhen;
    _options;
    _font = {
      fontFamily: "monospace",
      fontSize: "14px",
      fontWeight: "normal",
      color: DEFAULT_FG2,
      backgroundColor: DEFAULT_BG2,
      letterSpacing: ""
    };
    constructor(options) {
      this._options = {
        zIndex: options?.zIndex ?? DEFAULT_OPTIONS.zIndex,
        underlinePredictions: options?.underlinePredictions ?? DEFAULT_OPTIONS.underlinePredictions,
        ttlMs: options?.ttlMs ?? DEFAULT_OPTIONS.ttlMs,
        maxPending: options?.maxPending ?? DEFAULT_OPTIONS.maxPending,
        cursorGraceMs: options?.cursorGraceMs ?? DEFAULT_OPTIONS.cursorGraceMs,
        edgeMarginCells: options?.edgeMarginCells ?? DEFAULT_OPTIONS.edgeMarginCells,
        foregroundColor: options?.foregroundColor,
        backgroundColor: options?.backgroundColor
      };
      this._predictWhen = options?.predictWhen ?? null;
    }
    // ─── Lifecycle ────────────────────────────────────────────────────
    /** Called by `terminal.loadAddon()`. Do not call directly. */
    activate(terminal) {
      this._terminal = terminal;
      this._container = document.createElement("div");
      this._container.setAttribute("data-predictive-echo", "");
      this._container.style.cssText = `position:absolute;left:0;top:0;z-index:${this._options.zIndex};pointer-events:none`;
      const screen = terminal.element?.querySelector(".xterm-screen");
      if (screen) screen.appendChild(this._container);
      this._readFontStyle();
      if (typeof terminal.onWriteParsed === "function") {
        try {
          this._disposables.push(
            terminal.onWriteParsed(() => {
              if (this._reconcileScheduled) return;
              this._reconcileScheduled = true;
              queueMicrotask(() => {
                this._reconcileScheduled = false;
                this._anchorHold = false;
                this._safeReconcile();
              });
            })
          );
        } catch {
        }
      }
      if (typeof terminal.onResize === "function") {
        try {
          this._disposables.push(terminal.onResize(() => this.clearPredictions()));
        } catch {
        }
      }
    }
    dispose() {
      this.clearPredictions();
      for (const d of this._disposables) {
        try {
          d.dispose();
        } catch {
        }
      }
      this._disposables = [];
      this._container?.remove();
      this._container = null;
      this._terminal = null;
    }
    // ─── Public API ───────────────────────────────────────────────────
    /**
     * Predict a single typed character at the current insertion point.
     * Returns false when suppressed; the consumer sends the keystroke to the
     * PTY either way (the return value is informational, never a send gate).
     */
    predictChar(ch) {
      try {
        this._reconcile();
        if (this._anchorHold) return false;
        const t = this._terminal;
        if (!t || !this._container) return false;
        const dims = getCellDimensions(t);
        if (!dims) return false;
        const buf = t.buffer.active;
        if (typeof buf.cursorX !== "number" || typeof buf.cursorY !== "number") return false;
        if (buf.viewportY !== buf.baseY) return false;
        if (this._predictWhen && this._predictWhen(t) === false) return false;
        const cps = Array.from(ch);
        if (cps.length !== 1) return false;
        const cp = cps[0].codePointAt(0);
        if (cp < 32 || cp === 127) return false;
        const w = charCellWidth(t, cps[0]);
        if (w !== 1 && w !== 2) return false;
        if (w === 2 && !this._hasGetCell()) return false;
        if (this._outstanding.length >= this._options.maxPending) return false;
        if (this._outstanding.length === 0) {
          this._anchor = { row: buf.cursorY, col: buf.cursorX };
          this._cursorOffRowSince = null;
        }
        const anchor = this._anchor;
        const last = this._outstanding[this._outstanding.length - 1];
        const offset = last ? last.offsetCells + last.width : 0;
        const col = anchor.col + offset;
        if (col + w > t.cols - this._options.edgeMarginCells) return false;
        const rec = {
          seq: this._seq++,
          char: cps[0],
          width: w,
          offsetCells: offset,
          snapshot: this._readCell(anchor.row, col),
          sentAt: performance.now(),
          mismatches: 0
        };
        this._outstanding.push(rec);
        addPredictionSpan(this._container, this._spans, {
          seq: rec.seq,
          row: anchor.row,
          col,
          char: rec.char,
          width: w,
          dims,
          font: this._font,
          underline: this._options.underlinePredictions
        });
        this._armTtl();
        return true;
      } catch {
        return false;
      }
    }
    /**
     * Pop the newest outstanding prediction (visual only). Returns false when
     * none are outstanding. The consumer forwards \x7f UNCONDITIONALLY either
     * way; deleting already-echoed text renders at RTT.
     */
    predictBackspace() {
      try {
        const rec = this._outstanding.pop();
        if (!rec) {
          this._anchorHold = true;
          return false;
        }
        removePredictionSpan(this._spans, rec.seq);
        if (this._outstanding.length === 0) this._resetRun();
        return true;
      } catch {
        return false;
      }
    }
    /** Drop every outstanding prediction and its spans. Also arms the anchor
     *  hold: consumers clear on inputs (Enter, Esc, arrows, pastes) whose
     *  cursor effect is unknown until the next parsed write. */
    clearPredictions() {
      try {
        this._anchorHold = true;
        this._droppedTotal += this._outstanding.length;
        this._outstanding = [];
        clearAllSpans(this._spans);
        this._resetRun();
      } catch {
      }
    }
    /** Manual reconcile pass, for consumers without onWriteParsed. By contract
     *  it is called after writes parsed, so it also releases the anchor hold. */
    reconcile() {
      this._anchorHold = false;
      this._safeReconcile();
    }
    /** Swap the prediction gate at runtime (mirrors the buffer addon's setPrompt). */
    setPredictWhen(fn) {
      this._predictWhen = fn;
    }
    /** Re-read font/theme (call after skin or font-size changes). */
    refreshFont() {
      this._readFontStyle();
    }
    get hasPredictions() {
      return this._outstanding.length > 0;
    }
    get state() {
      return {
        outstanding: this._outstanding.length,
        confirmedTotal: this._confirmedTotal,
        droppedTotal: this._droppedTotal,
        anchor: this._anchor ? { ...this._anchor } : null
      };
    }
    // ─── Reconciliation ───────────────────────────────────────────────
    _safeReconcile() {
      try {
        this._reconcile();
      } catch {
      }
    }
    _reconcile() {
      const t = this._terminal;
      if (!t) return;
      if (this._outstanding.length === 0) return;
      const buf = t.buffer.active;
      if (buf.viewportY !== buf.baseY) {
        this.clearPredictions();
        return;
      }
      if (typeof buf.cursorX !== "number" || typeof buf.cursorY !== "number") return;
      const anchor = this._anchor;
      const now = performance.now();
      if (buf.cursorY !== anchor.row) {
        this._cursorOffRowSince ??= now;
        if (now - this._cursorOffRowSince > this._options.cursorGraceMs) {
          this.clearPredictions();
          return;
        }
      } else {
        this._cursorOffRowSince = null;
      }
      while (this._outstanding.length > 0) {
        const rec = this._outstanding[0];
        const cell = this._readCell(anchor.row, anchor.col + rec.offsetCells);
        if (cell === rec.char && buf.cursorY === anchor.row && buf.cursorX >= anchor.col + rec.offsetCells + rec.width) {
          this._outstanding.shift();
          removePredictionSpan(this._spans, rec.seq);
          this._confirmedTotal++;
        } else {
          break;
        }
      }
      let dropFrom = -1;
      for (let i = 0; i < this._outstanding.length; i++) {
        const rec = this._outstanding[i];
        const cell = this._readCell(anchor.row, anchor.col + rec.offsetCells);
        if (cell !== rec.snapshot && cell !== rec.char && cell !== " ") {
          rec.mismatches++;
          if (rec.mismatches >= 2) {
            dropFrom = i;
            break;
          }
        } else {
          rec.mismatches = 0;
        }
      }
      if (dropFrom !== -1) this._dropFrom(dropFrom);
      for (let i = 0; i < this._outstanding.length; i++) {
        if (now - this._outstanding[i].sentAt > this._options.ttlMs) {
          this._dropFrom(i);
          break;
        }
      }
      if (this._outstanding.length === 0) {
        this._resetRun();
      } else {
        this._armTtl();
      }
    }
    _dropFrom(index) {
      const dropped = this._outstanding.splice(index);
      for (const rec of dropped) removePredictionSpan(this._spans, rec.seq);
      this._droppedTotal += dropped.length;
    }
    _resetRun() {
      this._anchor = null;
      this._cursorOffRowSince = null;
      if (this._ttlTimer !== null) {
        clearTimeout(this._ttlTimer);
        this._ttlTimer = null;
      }
    }
    _armTtl() {
      if (this._ttlTimer !== null) return;
      const oldest = this._outstanding[0];
      if (!oldest) return;
      const delay = Math.max(0, oldest.sentAt + this._options.ttlMs - performance.now()) + 1;
      this._ttlTimer = setTimeout(() => {
        this._ttlTimer = null;
        this._safeReconcile();
        this._armTtl();
      }, delay);
    }
    // ─── Cell access ──────────────────────────────────────────────────
    _hasGetCell() {
      const buf = this._terminal?.buffer.active;
      if (!buf) return false;
      const line = buf.getLine(buf.baseY + (buf.cursorY ?? 0));
      return typeof line?.getCell === "function";
    }
    /** Read one cell's chars at (viewport-relative row, col); '' -> ' '. */
    _readCell(row, col) {
      const buf = this._terminal.buffer.active;
      const line = buf.getLine(buf.baseY + row);
      if (!line) return " ";
      if (typeof line.getCell === "function") {
        const chars = line.getCell(col)?.getChars() ?? "";
        return chars === "" ? " " : chars;
      }
      const text = line.translateToString(true);
      return text[col] ?? " ";
    }
    // ─── Font ─────────────────────────────────────────────────────────
    /** Same recipe as the buffer addon's _cacheFont (kept private on purpose:
     *  zerolag-input-addon.ts must stay untouched by this feature). */
    _readFontStyle() {
      const t = this._terminal;
      if (!t) return;
      this._font.fontFamily = t.options.fontFamily || "monospace";
      this._font.fontSize = (t.options.fontSize || 14) + "px";
      this._font.fontWeight = String(t.options.fontWeight || "normal");
      this._font.backgroundColor = this._options.backgroundColor ?? t.options.theme?.background ?? DEFAULT_BG2;
      this._font.color = this._options.foregroundColor ?? t.options.theme?.foreground ?? DEFAULT_FG2;
      this._font.letterSpacing = "";
      const rows = t.element?.querySelector(".xterm-rows");
      if (rows) {
        const cs = getComputedStyle(rows);
        this._font.letterSpacing = cs.letterSpacing;
        if (!this._options.foregroundColor && cs.color) this._font.color = cs.color;
      }
    }
  };
  return __toCommonJS(index_exports);
})();
// Global aliases for browser usage
if(typeof window!=="undefined"){
  window.ZerolagInputAddon=XtermZerolagInput.ZerolagInputAddon;
  window.LocalEchoOverlay=class extends XtermZerolagInput.ZerolagInputAddon{
    constructor(terminal){
      super({prompt:{type:"character",char:"\u276f",offset:2}});
      this.activate(terminal);
    }
  };
  window.PredictiveEchoAddon=XtermZerolagInput.PredictiveEchoAddon;
  window.PredictiveEchoOverlay=class extends XtermZerolagInput.PredictiveEchoAddon{
    constructor(terminal){
      super({});
      this.activate(terminal);
    }
  };
}

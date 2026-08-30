# herdr-arrange — design & implementation plan

Interactive popup UI for herdr pane move / swap / re-split / layout.

Target: herdr **0.7.4+** (verified against 0.8.2, protocol 20, socket schema v1).

---

## 1. Language: Go

**Recommendation: Go + Bubble Tea.** Confirmed as a good fit, not just a preference:

- The plugin is ~80% TUI. Bubble Tea + Lip Gloss + `bubbles/viewport` is a mature stack
  for exactly this shape of app (modal keymap, tree list, live redraw).
- The entire herdr API is reachable with `net.Dial("unix", …)` + `encoding/json` —
  **zero non-UI dependencies**.
- One static binary, no runtime for the user to install (unlike Node/Bun/Python plugins).
- `[[build]]` in the manifest runs `go build` at install time on the user's machine, so
  no cross-compilation and no shipped binaries. Only prerequisite to document: Go ≥ 1.22.
- Rust would match herdr itself (ratatui), but nothing here needs it and it is slower to iterate.

Total dependency set: `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `charmbracelet/bubbles`.

**Platforms: `["macos", "linux"]`** for v1. Windows needs `Microsoft/go-winio` for the
named-pipe transport behind a build tag — deferred, since the layout methods we depend on
have no CLI wrapper, so we cannot fall back to shelling out to `HERDR_BIN_PATH` there.

---

## 2. What herdr actually gives us (verified, not assumed)

I probed the live 0.8.2 server and read the relevant handlers. Findings:

### Transport
- Newline-delimited JSON over `$HERDR_SOCKET_PATH`.
- **One request per connection.** The server writes one response line and closes; a second
  write on the same connection gets `EPIPE`. → the Go client must dial per call.
  (`src/api/client.rs`, confirmed empirically.)

### Usable primitives

| Need | Method | Verified behaviour |
|---|---|---|
| read tree | `layout.export` | BSP tree: `{type:"split",direction:"right"\|"down",ratio,first,second}` / `{type:"pane",pane_id,label,cwd,command}` |
| read everything | `session.snapshot` | workspaces + tabs + panes + per-tab layouts (rects/splits) in **one** call — the whole tree view from one request |
| h/j/k/l swap | `pane.swap` | same-tab only; preserves shape, ratios, pane ids, **processes**. Response `focused_pane_id` = source pane, i.e. focus follows the pane. |
| set ratios | `layout.set_split_ratio` | `{tab_id, path:[bool], ratio}`, `false`=first / `true`=second. **Ratio is clamped to [0.1, 0.9]** (`Layout::set_ratio_at`). |
| move across tabs | `pane.move` | `destination: {type:"tab", tab_id, target_pane_id, split:"right"\|"down", ratio}`. Preserves pane id, terminal id and pid. |
| new tab / workspace | `pane.move` | `destination: {type:"new_tab"\|"new_workspace"}`; auto-closes the source tab when the last pane leaves (`closed_tab_id` in the response). |
| popup UI | `plugin.pane.open` | `placement:"popup"`, `width`/`height` as cells or `"NN%"`; oversized values clamp down to the terminal area. |
| focus | `pane.focus` / `tab.focus` / `workspace.focus` | `{pane_id}` etc. |
| errors to user | `notification.show` | for `ui_busy` and friends |

### The hard constraint

There is **no non-destructive tree-restructure API**:

- `layout.apply` always creates fresh panes. `LayoutPane.pane_id` is output-only and ignored
  on apply (`src/app/api/layouts.rs:34-219`); the docs confirm it "does not preserve live
  PTYs, scrollback, or running processes". **Unusable for us** — it would kill the agents.
- `pane.move` into the *source* tab returns `changed:false, reason:"same_tab"`. The docs say
  "same-tab layout changes remain `pane.swap`", and `pane.swap` cannot change shape.

So `H/J/K/L` re-split and the `1`–`5` layout presets have no direct API.

### The verified workaround: park-and-reinsert

Cross-tab `pane.move` *does* preserve everything. So restructuring in place =
move panes out to a temporary tab, then move them back one at a time at the right
position. I ran this end-to-end on a scratch workspace:

```
start:  [p1 | [p2 / p3]]  (right split, p1 left)
1. pane.move p3 → new_tab            → p3 in w1T:t2, pane_id AND terminal_id unchanged
2. pane.move p3 → tab t1, target=p2, split=down, ratio=0.3
                                     → tree becomes [p1 | [p2 / p3]] with ratio 0.3
                                     → scratch tab auto-closed (closed_tab_id: w1T:t2)
                                     → pane.process_info: shell_pid 47832, unchanged
```

Also verified: `layout.set_split_ratio` on `path:[]` and `path:[true]`, and explicit
`pane.swap` — all work over the raw socket.

Cost: one scratch tab briefly appears in the tab strip, and ~2·(N−1) pane moves.
**Approved.** A herdr feature request for a proper in-place `layout.rearrange` is worth
filing separately; the reconciler below is the single place that would change.

### Popup behaviour (verified by linking a probe plugin)

- `HERDR_PLUGIN_CONTEXT_JSON` for a popup contains exactly what we need:
  `{"workspace_id":"w1T","workspace_label":…,"tab_id":"w1T:t1","focused_pane_id":"w1T:p1",
    "focused_pane_cwd":…,"invocation_source":"api","correlation_id":"plugin-pane"}`
  (no `HERDR_PANE_ID`, as documented — the popup is not a pane.)
- `TERM=xterm-256color`, real PTY, `stty` reports the popup's inner size, and
  `resize_popup_pane` re-sizes on terminal resize → SIGWINCH works. Bubble Tea is happy.
- **The popup receives every key, unconditionally**, ahead of all keybinding and prefix
  handling (`App::handle_key` → `prepare_popup_key_forward`, first branch, before any
  mode/keybind match). Keys are encoded with the normal terminal encoder, so `esc`,
  arrows and **shift+arrows** (`CSI 1;2A` …) all arrive. No conflict with user keybindings.
- The popup is rendered **last, over an undimmed background** (`ui.rs:420`), and layout
  mutations mark the frame dirty → the user sees swaps/presets land live behind the popup.
- Geometry: inner width = outer − 3, inner height = outer − 2. Minimum outer 6×4.
- Only one popup at a time; `plugin.pane.open` returns `ui_busy` if Settings/Copy mode/
  another modal is up.

---

## 3. Architecture

```
herdr-arrange/
  herdr-plugin.toml
  go.mod  go.sum
  main.go                     # dispatch: open | open-tree | ui | drain | call
  action.go                   # the open action and the startup drain
  internal/herdr/
    client.go                 # dial-per-request NDJSON client
    schema.go                 # request/response/param types
    api.go                    # typed wrappers per method
  internal/tree/
    tree.go                   # BSP model: Leaves(), Paths(), Find(), Clone(), Equal()
    presets.go                # even-h, even-v, main-h, main-v, tiled
    ops.go                    # ReSplit(dir), Even(), Equalize()
    reconcile.go              # Plan(cur, want) → []Step   ← the whole restructure story
  internal/engine/
    engine.go                 # one pane's operations: read tab, plan, execute, retarget
    journal.go                # the parking journal `drain` recovers from
  internal/ui/
    app.go                    # bubbletea root: mode switching, status line, error toast
    layout.go                 # layout mode view + keymap
    tree.go                   # tree-view mode view + keymap (viewport-scrolled)
    geometry.go               # the popup size each view needs, for the action to ask for
    theme.go                  # lipgloss styles
  README.md
```

Three processes, one binary:

| invocation | kind | job |
|---|---|---|
| `arrange open` | `[[actions]]` | read snapshot → decide initial mode → compute popup size → `plugin.pane.open` with `env: {ARRANGE_MODE, ARRANGE_PANE, ARRANGE_TAB, ARRANGE_WS}` |
| `arrange open-tree` | `[[actions]]` | same, forced `ARRANGE_MODE=tree` |
| `arrange ui` | `[[panes]]` popup | the Bubble Tea app |
| `arrange drain` | `[[startup]]` | crash-recovery: drain a leftover parking tab (§7) |

The actions must speak the socket directly: `herdr plugin pane open` has no `popup`
placement (CLI enum is `overlay|split|tab|zoomed`) and `layout.*` has no CLI wrapper at all.

### Manifest

```toml
id = "soh.arrange"
name = "Arrange"
version = "0.1.0"
min_herdr_version = "0.7.4"       # popup placement (0.7.4); layout.set_split_ratio (0.7.2)
description = "Interactive pane move, swap, re-split and layout"
platforms = ["macos", "linux"]

[[build]]
command = ["go", "build", "-trimpath", "-o", "bin/arrange", "."]

[[startup]]
command = ["bin/arrange", "drain"]

[[actions]]
id = "open"
title = "Arrange panes"
contexts = ["pane"]
command = ["bin/arrange", "open"]

[[actions]]
id = "open-tree"
title = "Arrange: move pane to…"
contexts = ["pane"]
command = ["bin/arrange", "open-tree"]

[[panes]]
id = "ui"
title = "Arrange"
placement = "popup"
width = 63
height = 14
command = ["bin/arrange", "ui"]
```

Popup size is set per-open by the action (overriding the manifest default above), because
the two modes need different heights. herdr's border eats 3 columns and 2 rows of the outer
size, and `internal/ui/geometry.go` derives the outer size from what the views render:

- layout mode: `63 x 14`, from the help panel's `60 x 12`.
- tree mode: the same width, `rows + 5 + 2` tall — the whole session with no scrolling —
  floored at layout mode's height (`t` switches views inside the popup we were given) and
  capped at 30 rows. **Not** `min(rows, 80%)` as first planned: nothing in the API reports
  the terminal size, so the action cannot compute a percentage of it. Oversized cell counts
  clamp down safely, so the cap is only there to keep a fifty-pane session from asking for
  the whole screen.

Beyond the cap, and whenever herdr clamps us, **tree mode scrolls** via `bubbles/viewport`
and layout mode drops help lines from the bottom up.

Keybindings are the user's to add (plugins cannot register keys). README will document:

```toml
[[keys.command]]
key = "prefix+a"
type = "plugin_action"
command = "soh.arrange.open"
description = "arrange panes"

[[keys.command]]
key = "prefix+A"
type = "plugin_action"
command = "soh.arrange.open-tree"
description = "move pane to…"
```

---

## 4. Layout mode

```
 h/j/k/l  ←↓↑→        swap pane
 H/J/K/L  shift+←↓↑→  re-split pane
 1 even-horizontal   2 even-vertical   3 main-horizontal
 4 main-vertical     5 tiled           space cycle presets
 e  even out pane sizes
 c  move pane to a new tab in this workspace
 N  move pane to a new workspace
 t  move/swap to another workspace/tab
 enter / esc  close
─────────────────────────────────────────────────
 w1S:t1 · 4 panes · main-vertical · this pane: p2
```

Behaviour per key:

- **`h/j/k/l` / arrows** → one `pane.swap {pane_id, direction}`. No tree math, no flicker.
  If `reason:"no_neighbor"`, flash the status line and do nothing.
- **`H/J/K/L` / shift+arrows** → `tree.ReSplit`, then reconcile. Semantics (confirmed):
  P detaches, its parent collapses to the sibling subtree S, and P re-attaches as a sibling
  of **all of S** on the chosen side, with ratio `1/(leaves(S)+1)`:
  ```
  [A | [P / B]]   --H-->   [A | [P | B]]
  ```
  Repeated presses walk P up one level at a time; at the root it becomes a full tab edge.
  If P is already the whole-tab-edge child in that direction, no-op with a status flash.
- **`1`–`5`** → build the preset tree over the tab's current panes, then reconcile.
- **`space`** → structurally match the current tree against all five presets and apply the
  next one (stateless; falls back to `1` when nothing matches). The status line names the
  detected preset so `space` is predictable.
- **`e`** → `tree.Even`: every pane the same size, like tmux's balance. Equal width and
  height only exists along one axis, so the axis is the root split's direction, and:
  a tab whose splits all run that way keeps its shape and gets its ratios re-weighted to
  `leaves(first)/leaves(node)` (pure `layout.set_split_ratio` — no flicker, no pane moves,
  `tree.Equalize`); a tab that mixes axes cannot be evened out by any ratio, so it is
  rebuilt as a balanced row or column and the status line says *rebuilt* rather than
  *evened out*. (Caveat: herdr clamps ratios to [0.1, 0.9], so a right-nested chain of >10
  panes cannot be made exactly even without a rebuild it does not need. The presets avoid
  this by building balanced trees, see §5.)
- **`c`** → `pane.move {destination:{type:"new_tab", workspace_id: current}, focus:true}`,
  then retarget layout mode at the new tab.
- **`N`** → `pane.move {destination:{type:"new_workspace"}, focus:true}`. **The response
  carries a new public pane id** (pane ids are workspace-scoped, `w1S:p2` → `w2A:p1`), so
  re-read `move_result.pane.pane_id` and retarget.
- **`t`** → tree-view mode.
- **`enter`/`esc`** → exit 0; herdr tears the popup down.

After every mutation: re-`layout.export`, re-render the help footer, and `pane.focus` the
tracked current pane so herdr's focus keeps following it.

---

## 5. Preset trees

herdr has **no built-in layout presets** — no `even-horizontal`/`tiled` anywhere in the
source. We define them. All presets are built as **balanced** binary trees rather than
right-nested chains: visually identical (N equal columns is N equal columns), but every
ratio stays near 0.5 and well inside herdr's [0.1, 0.9] clamp, and tree depth stays
`O(log n)`.

```go
// balanced(panes, dir): ratio = len(left)/len(all)
func balanced(ps []PaneID, dir Dir) *Node

evenHorizontal(ps)  = balanced(ps, Right)
evenVertical(ps)    = balanced(ps, Down)
mainVertical(ps)    = split(Right, 0.5, leaf(main), balanced(rest, Down))
mainHorizontal(ps)  = split(Down,  0.5, leaf(main), balanced(rest, Right))
tiled(ps)           = cols=ceil(√n); rows chunked; balanced(rows, Down) of balanced(row, Right)
```

**`main` = the current pane** (the one the popup was opened from) — confirmed. So `4` means
"make *this* pane the big left column". `rest` keeps the tab's existing in-order order.

Single-pane tabs: presets are no-ops.

---

## 6. The reconciler

One function drives every structural change. `Plan(cur, want)` picks the cheapest correct
strategy:

```go
switch {
case sameShape(cur, want) && samePaneOrder(cur, want):
    // ratios only
    → []SetRatio
case sameShape(cur, want):
    // leaf permutation → cycle-decompose into transpositions
    → []Swap (≤ n-1)  ++  []SetRatio
default:
    → Rebuild(want)
}
```

`Rebuild` (the park-and-reinsert algorithm, mirroring herdr's own
`apply_layout_node_to_pane` but with `pane.move` instead of `split`):

```go
leaves := want.InOrderLeaves()
anchor := leaves[0]              // stays put; guarantees the tab never empties
park   := leaves[1:]

// 1. park — first move creates the tab, the rest target it
for i, p := range park {
    if i == 0 { scratch = paneMove(p, NewTab{ws, label: "arrange:parking"}).TabID }
    else      { paneMove(p, Tab{scratch, split: Down}) }
}

// 2. materialize: host is always the in-order-first leaf of `node`
func materialize(node, host) {
    if node.IsLeaf() { return }           // invariant: node.leaf == host
    p := node.Second.FirstLeaf()
    paneMove(p, Tab{tab, targetPane: host, split: node.Dir, ratio: node.Ratio})
    materialize(node.First,  host)        // host became `first` of the new split
    materialize(node.Second, p)           // p became `second`
}
materialize(want, anchor)
// scratch tab auto-closes when its last pane leaves

// 3. exact ratios (pane.move ratios are approximate once children are added)
for path, r := range want.Ratios() { setSplitRatio(tab, path, r) }

// 4. restore
paneFocus(current); if wasZoomed { paneZoom(current, "on") }
```

Correctness notes:
- `split_at` in herdr makes the **target** the `first` child and the moved pane the
  `second` (`src/layout.rs:590`), which is exactly what `materialize` assumes.
- The invariant "host == in-order-first leaf of node" holds because `anchor = leaves[0]`
  and each recursion passes `host` down the `first` branch.
- Any binary tree over N leaves is reachable in N−1 leaf splits, so `Rebuild` can express
  every target tree — which is why `H/J/K/L` and the presets can share one code path.

Cost for a 6-pane tiled rebuild: 5 + 5 moves + ~5 ratio sets + 2 reads ≈ 17 dials,
each sub-millisecond locally. The UI shows a `working…` state and disables input while a
plan is executing.

---

## 7. Edge cases and safety

| Case | Handling |
|---|---|
| **Zoomed tab** | `pane.move` refuses with `reason:"zoomed_tab"`. Read `zoomed` from `layout.export`; if set, `pane.zoom off` before a rebuild and restore after. `pane.swap` works while zoomed (mutates the hidden tree) — leave it alone. |
| **Rebuild fails mid-plan** | Panes are stranded in the parking tab. (a) best-effort inline rollback: chain everything back into the target tab; (b) before parking, write `{tab_id, pane_ids, ws_id}` to `$HERDR_PLUGIN_STATE_DIR/parking.json` and delete it on success; (c) `[[startup]] arrange drain` reads that file on the next server start and moves the panes home. The parking tab is labelled `arrange:parking` so manual recovery is obvious too. |
| **`ui_busy`** on open | Action calls `notification.show` ("Arrange: close the current herdr dialog first") and exits non-zero. Shows up in `herdr plugin log list`. |
| **Single-pane tab** | `open` starts in tree-view (per spec). Layout mode still reachable via `t`→enter-on-current-pane→no-op, so tree mode's "enter on current pane" is a no-op there, as specified. |
| **Ratio clamp [0.1, 0.9]** | Balanced preset trees keep ratios ≈0.5. `e` on a pathological hand-built chain can't reach exact evenness; status line says so rather than silently lying. |
| **Cross-workspace pane id change** | Always re-read `move_result.pane.pane_id` after `new_workspace` moves. |
| **Pane closed / tab closed under us** | Every action re-reads `layout.export` first; a missing current pane closes the popup with a message rather than erroring. |
| **Popup is not a pane** | It never appears in the tree view, has no `HERDR_PANE_ID`, and is excluded from pane APIs — the current pane comes from `HERDR_PLUGIN_CONTEXT_JSON` / the `ARRANGE_PANE` env the action passes. |
| **Terminal too small** | `resolve_popup_geometry` returns `None` below 6×4 and `plugin.pane.open` fails; surface via `notification.show`. |

---

## 8. Tree-view mode

Single `session.snapshot` call builds everything. Rows are a flat list with a `kind`
(`workspace | tab | pane | new-tab | new-workspace`), rendered with box-drawing prefixes
and scrolled in a `bubbles/viewport`.

```
[1] w1S  herdr-arrange                    ← new tab in workspace herdr-arrange
    ├─ t1  main
    │  ├─ p2  claude  (current)           ← dimmed
    │  └─ p5  nvim
    ├─ t2  logs
    │  └─ p7  tail
    └─ [c] new tab in this workspace       ← only under the current workspace
[2] wJ   notes
    └─ …
[N] new workspace
────────────────────────────────────────────────────────────
 j/k ↓↑ move   enter apply   c new tab here   1-9 workspace   t/esc back
 → swap this pane with wJ:p1
```

- Current workspace / tab / pane are **dimmed** (as specified) and the current pane is
  marked `(current)`.
- The bottom line always states exactly what `enter` will do — one of
  *new tab in workspace X* / *move this pane to tab X* / *swap this pane with pane X* /
  *back to layout mode*.
- Selection starts on the current pane.
- `1`–`9` = that workspace's "new tab here" action directly. `c` = new tab in the current
  workspace. `N` = new workspace.
- After a **tab/new-tab** action → focus the destination and switch to **layout mode** on it.
  After a **pane swap** action → exit (per spec). After **enter on the current pane** →
  layout mode if the tab has >1 pane, else no-op.

Cross-workspace tab moves are `pane.move {destination:{type:"tab", tab_id, split:"down"}}`;
"new tab in workspace X" is `pane.move {destination:{type:"new_tab", workspace_id:X}}`
(one call — no separate `tab.create` needed, and it auto-closes an emptied source tab).

---

## 9. Milestones

1. **Socket client + schema** (`internal/herdr`). Dial-per-request, typed wrappers, a
   `-json` debug mode on the binary for manual poking. Table tests against a fake server.
2. **Tree model + presets + ops + reconciler** (`internal/tree`). Pure functions, no I/O —
   this is where the real correctness risk lives, so it gets the bulk of the unit tests:
   round-trip `export → plan → simulate → equals want` for random trees up to 12 leaves,
   plus golden tests for each preset at n = 1…8.
3. **Executor**: `[]Step` → socket calls, with the parking-tab journal, zoom save/restore
   and focus restore. Integration-tested against a real server in a scratch workspace
   (the harness I used above, promoted to `scripts/e2e.sh`).
4. **Layout mode UI**: keymap, help panel, status line, live re-export after each action.
5. **Tree-view mode UI**: snapshot → rows, viewport scrolling, action preview line.
6. **Actions + manifest + startup drain**: mode selection, popup sizing, `ui_busy` handling.
7. **README**: install, the two keybindings, the Go prerequisite, the parking-tab note,
   and a short "why a scratch tab flashes" explanation.

Steps 1–3 are the load-bearing parts and are fully testable without a TUI; 4–5 are
mechanical once the reconciler is trustworthy.

## 10. Open follow-ups (not v1)

- Windows support via `go-winio`.
- File a herdr feature request for in-place `layout.rearrange` (reorder existing panes
  without cross-tab moves) — would delete the parking machinery from `reconcile.go`.
- `u` to undo the last structural change (cheap: keep the previous exported tree and
  re-reconcile toward it).
- Mouse support in the tree view (`plugin.pane.open` popups do receive mouse reports).

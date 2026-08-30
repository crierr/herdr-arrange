# herdr-arrange

Interactive pane move, swap, re-split and layout for [herdr](https://herdr.dev), in a popup.

Two views. **Layout mode** rearranges the panes of the current tab; **tree mode** sends the
pane you are on to another tab or workspace. Everything moves live behind the popup, and
nothing is respawned: pane ids, terminals, scrollback and running agents all survive.

```
 h/j/k/l  ←↓↑→         swap pane
 H/J/K/L  shift+←↓↑→   re-split pane
 1 even-horizontal   2 even-vertical     3 main-horizontal
 4 main-vertical     5 tiled             space cycle presets
 e             even out pane sizes
 c             move pane to a new tab in this workspace
 N             move pane to a new workspace
 t             move/swap to another workspace/tab
 enter / esc   close
────────────────────────────────────────────────────────────
 w1S:t1 · 4 panes · main-vertical · this pane: p2
 applied main-vertical
```

## Install

Needs herdr 0.7.4+, macOS or Linux, and **Go 1.24.2 or newer on `PATH`** — the plugin is
one Go binary, built on your machine at install time rather than shipped.

```sh
herdr plugin install crierr/herdr-arrange
```

Then bind it. Plugins cannot register keys, so this part is yours; add it to
`~/.config/herdr/config.toml`:

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

`herdr config check` will tell you if it disagrees. Both actions also show up in herdr's
own plugin action list, so you can try them before binding anything:

```sh
herdr plugin action invoke soh.arrange.open
```

To work on the plugin instead of installing a release:

```sh
git clone git@github.com:crierr/herdr-arrange.git
cd herdr-arrange && go build -trimpath -o bin/arrange .
herdr plugin link "$PWD"
```

## Layout mode

Opens on the tab you are in, with the pane you are on as the one being arranged.

| key | what it does |
|---|---|
| `h` `j` `k` `l`, arrows | swap this pane with its neighbour that way |
| `H` `J` `K` `L`, shift+arrows | re-split: detach this pane and re-attach it along that edge |
| `1`–`5` | apply a layout preset, with **this** pane as the main one |
| `space` | apply the next preset after the one the tab already matches |
| `e` | make every pane the same size: equal columns, or equal rows |
| `c` | move this pane to a new tab in this workspace |
| `N` | move this pane to a new workspace |
| `t` | switch to tree mode |
| `enter`, `esc`, `q` | close |

The status line names the layout the tab currently matches — relative to the pane being
arranged, so it is always the one the matching number key would reproduce. `custom` means
no preset matches.

`H/J/K/L` walk outwards. Pressing `H` on `[A | [P / B]]` gives `[A | [P | B]]`; pressing it
again gives `[P | [A | B]]`, and once the pane owns a whole tab edge, further presses that
way do nothing.

`e` is tmux's balance: every pane the same size, as equal columns or equal rows. Which of
the two comes from how the tab is divided at the top level, so `e` never turns a layout on
its side — a stack of rows evens out as rows.

Equal width *and* height only exists along a single axis, so what `e` costs depends on the
shape it starts from. A tab that already runs one way, however deeply nested, needs nothing
but new split ratios, so it is instant and no pane moves:

```
[a |0.8 [b |0.9 [c | d]]]   -->   four equal columns, ratios only
```

A tab that mixes both axes cannot be made even by any ratio at all, so `e` rebuilds it as a
plain row (or column) and says *rebuilt as 4 equal columns* — that one flickers, the way the
preset keys do:

```
[[a | b] | [c / d]]   -->   [[a | b] | [c | d]]   four equal columns
```

Pressing `e` on a tab that is already like that says *already 4 equal columns*. And because
`even-horizontal` and `even-vertical` are exactly what `e` produces, `e` on those two presets
has nothing to do — while on `main-horizontal` and `main-vertical` it shrinks the main pane
from its half of the tab to `1/n`, which does undo what the preset was for.

herdr clamps split ratios to `[0.1, 0.9]`, so a hand-built chain of ten or more columns
cannot be evened out by ratios alone: there `e` says it did so *as far as herdr's ratio
limits allow* rather than claiming an evenness it cannot deliver.

## Tree mode

`prefix+A` opens straight here, and so does `prefix+a` on a tab with only one pane — there
is no layout to arrange there, but the pane can still go somewhere.

```
  [1] w1S  herdr-arrange
  ├─ t1  main  4 panes
  │  ├─ p1
▸ │  ├─ p2  claude  (current)
  │  ├─ p3
  │  └─ p4
  ├─ t2  logs  1 pane
  │  └─ p7  tail
  └─ [c] new tab in this workspace
  [2] wJ  notes
  └─ t1  1 pane
     └─ p1
  [N] new workspace
────────────────────────────────────────────────────────────
 j/k move  enter apply  t layout  esc close
 c new tab here  1-9 workspace  N new workspace
 → swap this pane with p3
```

`j`/`k` and the arrows move; `g`/`G`, `home`/`end`, `pgup`/`pgdown` and `ctrl+f`/`b`/`d`/`u`
work too. `1`–`9` jump straight to a workspace's action, `c` and `N` to the two synthetic
rows at the bottom. The last line always says exactly what `enter` will do:

| selected row | `enter` |
|---|---|
| a workspace | move this pane to a new tab in that workspace, then arrange it there |
| a tab | move this pane into that tab, then arrange it there |
| a pane in this tab | swap the two panes, and close |
| a pane in another tab | move this pane next to that one, then arrange it there |
| the pane you are moving | back to layout mode |

A pane in another tab is offered as somewhere to *land*, not as a swap, because herdr's
`pane.swap` cannot cross a tab boundary — and a swap that killed a terminal to fake it
would be worse than being told what will actually happen.

## Why a tab flashes past sometimes

herdr has no API for restructuring a tab's tree in place. `layout.apply` respawns panes,
which would kill whatever is running in them, and `pane.move` back into the same tab is
refused. What *is* safe is moving a pane between tabs: that keeps the pane id, the terminal
and the process.

So a re-split or a preset — anything that changes the tab's shape rather than just its
ratios — is done by parking panes in a scratch tab called `arrange:parking` and moving them
back one at a time in the right order. herdr closes the scratch tab when its last pane
leaves. That is the flicker, and it is why a rearrange takes a moment while a swap
(`h/j/k/l`) is instant, and `e` is instant whenever it only has to change ratios: no
restructuring, nothing to park.

If herdr is killed halfway through one of those plans, the panes are still alive, just in the
wrong tab. Before parking anything the plugin writes what it is about to do to
`$HERDR_PLUGIN_STATE_DIR/parking.json`, and its `[[startup]]` hook (`arrange drain`) moves
any stranded panes home the next time herdr starts, then says so. If the tab they came from
is gone by then, they land in a new tab called `arrange:recovered` rather than being guessed
at. You can also just drag them back by hand — the tab name is there to make that obvious.

## Debugging

```sh
herdr plugin log list              # what the actions did, and why one failed
bin/arrange call session.snapshot  # send any API method, pretty-print the reply
```

The popup grabs every keystroke ahead of herdr's own bindings while it is open, so if it
ever gets stuck, `esc` closes it and killing `arrange ui` closes it from outside.

## Not in v1

- Windows: the socket needs a named-pipe transport, and the layout methods this depends on
  have no CLI wrapper to fall back to.
- Undo, and mouse support in the tree view.

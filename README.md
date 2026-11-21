# vMix — jadeck add-on

Switch, transition, overlay, record and stream, mix audio, edit titles,
play media and run replay in [vMix](https://www.vmix.com) from jadeck
dashboards, macros and the assistant. Works with vMix 29 (the API map was
taken from vMix 29.0.0.50; older versions with the same API work too).

## Quick setup

1. In vMix: **Settings → Web Controller**, tick **Enabled** (8088) and
   **TCP API Enabled** (8099), then OK.
2. If jadeck reaches vMix over Tailscale or another routed network, untick
   **"Restrict access to LAN only"** (both boxes). On the same LAN, leave it.
3. In jadeck: Settings → Connections → Add → vMix.
4. Open the **vMix PC address** list: vMix PCs on this computer's networks
   are found in a few seconds ("192.168.1.30 — vMix 29 4K"). Pick one, or
   type the address (Tailscale and VPN addresses are not scanned).
5. Fill in the Web Controller password only if the list says
   "(password set)", then press **Test**: it answers in one line, and says
   what to change if something is off.

## Set up vMix

In vMix: **Settings → Web Controller**.

1. **Web Controller: Enabled** (port 8088). Functions and full state.
2. **TCP API: Enabled** (port 8099). The live link: tally and changes are
   pushed to jadeck the moment they happen. Without it jadeck still works
   over the Web Controller, but reads state only when a widget asks.
3. **"Restrict access to LAN only"** is ticked by default, once for the
   Web Controller and once for the TCP API. vMix then only lets in
   `10.x`, `172.16-31.x` and `192.168.x` addresses (and the PC itself); any
   other client is accepted and dropped at once. **Tailscale (`100.x`)
   addresses are dropped.** If jadeck reaches vMix over Tailscale or another
   routed network, untick both boxes. jadeck says so when it happens
   ("vMix accepted the connection and closed it straight away…").
4. **Password** (optional): set a username and password for the Web
   Controller and enter the same in jadeck. With no password vMix doesn't
   ask for one. The TCP API has no password at all — keep it on a trusted
   network.
5. Windows Firewall must allow vMix on those ports (vMix asks the first
   time).

## Add the connection in jadeck

Settings → Connections → Add → vMix.

| Field | |
|---|---|
| vMix PC address | pick from the list (vMix PCs found on this network), or `127.0.0.1` on the same PC, else its IP or name |
| Web Controller port | 8088 |
| TCP API port (advanced) | 8099 |
| Live updates (advanced) | TCP API (recommended) or Web Controller only |
| Username / password | only if set in vMix |
| Read-only | show state and tally, never change anything |

Run the **Check connection** action (probe) to see the vMix version and
edition, the number of inputs, and whether a password or the LAN-only
setting is in the way. It only reads.

Nothing is changed in vMix when jadeck connects.

## Widgets

Every widget has settings of its own (the widget's settings panel), and
each one fits any size, from a corner to a whole dashboard.

| Widget | Settings |
|---|---|
| **Inputs** (tiles like a multiview: red = program, green = preview) | which inputs and their order, show numbers, mix, what a tap does, hold time |
| **Input buttons** (the same as compact buttons) | same |
| **Transitions** | which buttons (Cut, Fade, Merge, Stinger, FTB, Transition 1-4), mix, duration, stinger |
| **T-bar** | — |
| **Overlays** | which channels, the input for each (blank = preview input), mix |
| **Outputs** (stream, record, external, MultiCorder… with times) | which outputs, which stream, hold time |
| **Output status**, **Recording clock** | recording or streaming |
| **Audio mixer** (faders, stereo LED meters, MUTE on every strip) | which strips and their order, meters, dB readout, mute, solo, faders |
| **Title editor** | which title input and which fields |
| **Title presets** | which title input |
| **Media** (progress, restart, play, pause, next, loop) | which input |
| **List** | which list input |
| **Replay**, **Tally** | mix (tally) |
| **Button pad** | any actions, in your order, with names and colours, hold time |

**Taking an input to program.** The Inputs widget's *Tap* setting:

- *Preview* (default): a tap puts the input in preview; take it with Cut
  or Fade. Safest.
- *Preview; tap the green one again to cut*: a second tap on the input in
  preview cuts it. A second tap within 0.6 s is treated as a double tap and
  ignored.
- *Cut, after a press-and-hold*: hold the tile; it fills red and counts
  down ("CUTS IN 3 S"); let go early and nothing happens.
- *Cut at once*: no confirm (for a program row).

**Hold to confirm.** Starting or stopping a stream, a recording, external
output or MultiCorder is pressed and **held** (default 5 s, the widget's
*Hold to confirm* setting, 3-10 s). The button fills in red and counts
down ("STOPS IN 3 S"); letting go or sliding off drains it and sends
nothing; Space or Enter held works too. No pop-ups, no second tap. As a
second safety the add-on refuses those four outputs unless the action
carries `confirmed: true`, which the dashboard sends only after the hold
(a macro step must set it itself).

vMix has no API for input thumbnails, so input tiles are shaded by input
type rather than showing pictures.

## Actions

Each action is a dashboard button, a macro step and an assistant tool.
Inputs can be given as number, title or key; they are always sent to vMix
as the key. Mix is 1-16 (1 = main).

`preview_input` · `program_input` · `cut` · `fade` · `merge` · `stinger` ·
`transition` · `transition_button` · `set_tbar` · `fade_to_black` ·
`overlay` · `overlay_all_off` · `output` · `snapshot` · `audio_volume` ·
`audio_mute` · `audio_solo` · `audio_bus` · `title_text` · `title_image` ·
`title_preset` · `media` · `list_select` · `replay` · `script` ·
`script_stop_all` · `ptz_preset` · `ptz_home` · `run_function` · `probe` ·
`refresh`

`run_function` runs any of vMix 29's 777 functions by name. It checks the
name and that only the parameters that function reads are given, with
values in range, before anything is sent. (`ScriptStartDynamic` is refused;
vMix blocks it over its API anyway.)

## Build

```bash
go test -race ./...
go run github.com/toolingdev/jadeck/sdk/addon/cmd/ptz-addon build
```

The tests run against a fake vMix (Web Controller and TCP API) in
`fake_test.go`. Try it on a controller: Settings → Add-ons → Developer →
Load a folder (this folder), or install `dist/vmix.ptzaddon`.

Release: push a tag `v1.2.3`; the workflow builds, signs (secret
`PTZ_SIGNING_KEY`) and attaches `vmix.ptzaddon` to the GitHub release.

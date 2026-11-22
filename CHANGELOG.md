# Changelog

## 2.2.0

- **Inputs: send an input to an overlay.** Press and hold a tile (or
  right-click it, or press O) and pick overlay 1-4 (1-8 in the widget's
  settings): the input goes on that overlay live; the red one (already on
  air there) takes it off. Tiles show which overlays they're on ("OV 2").
  On "cut after a press-and-hold" grids, the press stays the cut and a
  small ◧ button on each tile opens the menu. Settings: "Overlay menu"
  (on by default) and "Overlay channels in the menu".

## 2.1.0

- **Widgets use jadeck's shared frame kit** (`/api/kit/v1`, needs jadeck
  0.6 or newer). The Audio mixer now has the console fader strip (dB
  scale, metallic cap, LED meter next to the cap, level pill, MUTE / S),
  the same as the Behringer WING faders; strips that don't fit are counted
  in the header ("3 OF 5 STRIPS"). Buttons, hold to confirm and the header
  are the kit's in every widget. The add-on's own copies (`ui/kit.js`,
  `ui/kit.css`) are gone; the bridge is `ui/bridge.js`.

## 2.0.0

A full vMix control surface (vMix 29; the API map comes from vMix 29.0.0.50).
Needs jadeck 0.5.0 or newer (widgets v2, add-on SDK v0.3.0).

- **Live link over the TCP API (8099).** Tally and activators are pushed
  (`SUBSCRIBE TALLY`, `SUBSCRIBE ACTS`); the state XML is re-read over the
  same socket when something changes. PING keep-alive, reconnect with
  backoff. Functions go over the Web Controller (8088), which reports
  vMix's own error text; when it can't be reached they go over TCP.
- **Password and LAN-only.** Optional Web Controller username / password.
  A clear message when vMix's "Restrict access to LAN only" drops the
  connection (Tailscale 100.x addresses are dropped by default) or a
  password is needed.
- **Inputs by key.** Inputs are sent to vMix by their GUID, so moving or
  deleting inputs in vMix never sends the wrong one.
- **State:** every mix, overlays (all 16 channels), transitions 1-4, fade
  to black, recording / streaming (per stream) / external / MultiCorder /
  fullscreen / playlist with times, master and buses A-G (level, mute,
  solo, meters), per-input audio, title fields, list items, replay.
- **Hold to confirm:** starting / stopping streaming, recording, external
  and MultiCorder (and a held cut from the Inputs widget) is pressed and
  held, 3-10 s (default 5), with the button filling and counting down.
  Those outputs also need `confirmed: true` in the action.
- **Actions:** preview / cut on any mix (tap-again-to-take, or cut after a
  hold), every transition and stinger, T-bar, FTB, overlays, outputs, snapshots, volume / fades / mute /
  solo / bus routing, title text and images, title presets, media
  transport and seek, list items, replay, scripts, PTZ through vMix, any of
  the 777 functions (checked before sending), and a read-only probe.
- **Widgets**, each with its own settings: Inputs (picture tiles, or
  compact buttons), Transitions, T-bar, Overlays, Outputs, Output status,
  Recording clock, Audio mixer (faders, stereo LED meters with peak hold,
  MUTE on every strip), Title editor, Title presets, Media, List, Replay,
  Tally and a Button pad of any actions with your own names and colours.
- **Safe by default:** nothing changes in vMix on connect; a read-only
  connection option.

## 1.0.0

First release as its own add-on: cut, fade, preview, tally and live status
over the Web Controller.

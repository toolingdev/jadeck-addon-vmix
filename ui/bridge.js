// Shared bridge and helpers for the vMix frame widgets (design/WIDGETS-V2.md,
// bridge v1). Each page sets Kit.render(state, settings) and calls Kit.start().
// The controls (header, buttons, hold to confirm, fader strip, LED meter)
// come from jadeck's shared frame kit, window.JK (/api/kit/v1/kit.js,
// loaded before this file; design/WIDGETS-V2.md "Shared frame kit").
(function () {
  "use strict";
  var JK = window.JK, host = window.parent, seq = 0, pending = {};
  var K = window.Kit = { pendingDraw: false, state: {}, settings: {}, size: { w: 0, h: 0 }, editing: false, touch: false, render: function () {}, onSize: null, stateAt: Date.now() };

  function send(m) { host.postMessage(m, "*"); }
  if (!JK) { // a jadeck from before the shared frame kit
    K.start = function () {
      document.body.textContent = "This widget needs a newer jadeck (0.6 or later).";
      document.body.style.cssText = "color:#8d96a5;font:12px system-ui,sans-serif;display:flex;align-items:center;justify-content:center;text-align:center;height:100%;margin:0;padding:8px";
      send({ t: "ready" });
    };
    return;
  }
  function req(m) {
    var id = "r" + (++seq);
    m.id = id;
    return new Promise(function (ok, bad) {
      pending[id] = { ok: ok, bad: bad };
      send(m);
      setTimeout(function () { if (pending[id]) { delete pending[id]; bad(new Error("no answer")); } }, 6000);
    });
  }
  K.act = function (name, params) {
    if (K.editing) return Promise.resolve(null);
    return req({ t: "action", name: name, params: params || {} }).catch(function (e) { K.log(name + ": " + e.message); throw e; });
  };
  K.resource = function (path) { return req({ t: "resource", path: path }); };
  K.log = function (text) { send({ t: "log", text: String(text).slice(0, 300) }); };

  K.bool = function (k, d) { var v = K.settings[k]; return typeof v === "boolean" ? v : v === "true" ? true : v === "false" ? false : d; };
  K.list = function (v) { return Array.isArray(v) ? v.filter(function (x) { return typeof x === "string" && x; }) : typeof v === "string" && v ? v.split(",") : []; };
  K.el = function (tag, cls, text) { return JK.el(tag, cls, null, text); };
  K.clock = function (ms) {
    if (!(ms >= 0)) ms = 0;
    var s = Math.floor(ms / 1000), h = Math.floor(s / 3600), m = Math.floor(s / 60) % 60, x = s % 60;
    return (h ? h + ":" + (m < 10 ? "0" : "") : "") + m + ":" + (x < 10 ? "0" : "") + x;
  };
  K.hms = function (ms) {
    if (!(ms >= 0)) ms = 0;
    var s = Math.floor(ms / 1000), p = function (n) { return (n < 10 ? "0" : "") + n; };
    return p(Math.floor(s / 3600)) + ":" + p(Math.floor(s / 60) % 60) + ":" + p(s % 60);
  };
  // vMix volume 0-100 is gain^0.25 × 100 (vMix 29's own conversion), so dB = 80·log10(v/100).
  K.volDb = function (v) { return v > 0 ? 80 * Math.log10(Math.min(100, v) / 100) : -Infinity; };
  K.dbVol = function (db) { return 100 * Math.pow(10, db / 80); };
  K.ampDb = function (a) { return a > 0 ? 20 * Math.log10(a) : -Infinity; };
  // law: vMix's own fader travel (position = volume / 100) on the kit's
  // fader strip, 0 dB (volume 100) at the top; meters red from -6 dB,
  // amber from -18 dB.
  K.law = {
    toPos: function (db) { return db == null || db <= -144 ? 0 : Math.min(1, Math.pow(10, db / 80)); },
    toDb: function (pos) { return pos <= 0 ? -144 : Math.max(-144, Math.round(80 * Math.log10(Math.min(1, pos)) * 10) / 10); },
    ticks: [0, -5, -10, -20, -30, -40, -60, -144], hot: -6, warm: -18,
  };

  // K.press: a kit button that runs an action when tapped (busy until it answers).
  K.press = function (b, name, params) {
    b.set({ onClick: function () { return K.act(name, params).catch(function () {}); } });
  };
  // K.holdButton: a kit hold-to-confirm button that runs action(params).
  K.holdButton = function (label, sub, o) {
    return JK.holdButton({ label: label, sub: sub, seconds: o.seconds, tone: o.tone, verb: o.verb, holdLabel: o.holdLabel,
      active: o.active, onFire: function () { return K.act(o.action, o.params); } });
  };
  var hdr = null;
  K.header = function (title, ctx) {
    if (!hdr) hdr = JK.header({});
    hdr.set({ title: title, right: ctx || "" });
  };
  K.empty = function (main, text) { main.textContent = ""; main.appendChild(K.el("div", "jk-empty", text)); };

  // A widget never redraws under a finger that is holding a button.
  function draw() { if (JK.holds > 0) { K.pendingDraw = true; return; } JK.setEditing(K.editing); try { K.render(K.state, K.settings); } catch (e) { K.log("render: " + e.message); } }
  JK.onIdle = function () { if (K.pendingDraw) { K.pendingDraw = false; setTimeout(draw, 60); } };

  window.addEventListener("message", function (e) {
    if (e.source !== host) return;
    var m = e.data;
    if (!m || typeof m.t !== "string") return;
    switch (m.t) {
      case "init":
        K.state = m.state || {}; K.settings = m.settings || {}; K.size = m.size || K.size; K.editing = !!m.editing; K.touch = !!m.touch;
        K.stateAt = Date.now(); JK.theme(m.theme); draw(); break;
      case "state": K.state = m.state || {}; K.stateAt = Date.now(); draw(); break;
      case "size": K.size = m.size || K.size; if (K.onSize) K.onSize(); else draw(); break;
      case "theme": JK.theme(m.theme); break;
      case "result": {
        var p = pending[m.id];
        if (!p) return;
        delete pending[m.id];
        if (m.ok) p.ok(m.blob !== undefined ? m.blob : m.result); else p.bad(new Error(m.error || "failed"));
      }
    }
  });
  window.addEventListener("resize", function () { if (K.onSize) K.onSize(); else draw(); });
  K.start = function () { send({ t: "ready" }); };
})();

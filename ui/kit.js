// Shared bridge and helpers for the vMix frame widgets (design/WIDGETS-V2.md,
// bridge v1). Each page sets Kit.render(state, settings) and calls Kit.start().
(function () {
  "use strict";
  var host = window.parent, seq = 0, pending = {};
  var K = window.Kit = { holds: 0, pendingDraw: false, state: {}, settings: {}, size: { w: 0, h: 0 }, editing: false, touch: false, render: function () {}, onSize: null, stateAt: Date.now() };

  function send(m) { host.postMessage(m, "*"); }
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
  K.el = function (tag, cls, text) { var e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };
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
  K.dbText = function (d) { return d === -Infinity || d < -99 ? "−∞" : (d > 0.04 ? "+" : d < -0.04 ? "−" : "") + Math.abs(d).toFixed(1); };
  K.button = function (label, sub, cls) {
    var b = K.el("button", "btn live " + (cls || ""));
    b.type = "button";
    b.appendChild(K.el("span", "", label));
    if (sub) b.appendChild(K.el("small", "", sub));
    return b;
  };
  // K.hold makes el press-and-hold: o.seconds (default 5), o.fire() once
  // at the end, o.progress(secondsLeft) while held, o.cancel() when let go
  // early or slid off (the fill drains in ~300 ms). Space / Enter held work
  // the same. Vibrates when it fires, on phones that can.
  K.hold = function (el, o) {
    var secs = 5, t0 = 0, raf = 0, active = false, last = -1;
    var fill = K.el("i", "hold-fill " + (o.tone || "pgm"));
    el.insertBefore(fill, el.firstChild);
    el.style.position = el.style.position || "";
    function set(p) { fill.style.transform = "scaleX(" + p + ")"; }
    function tick() {
      if (!active) return;
      var p = Math.min(1, (performance.now() - t0) / (secs * 1000)), left = Math.max(1, Math.ceil(secs * (1 - p)));
      set(p);
      if (left !== last && p < 1) { last = left; if (o.progress) o.progress(left); }
      if (p >= 1) { active = false; fire(); return; }
      raf = requestAnimationFrame(tick);
    }
    function start() {
      if (K.editing || el.disabled || active) return;
      secs = Math.max(1, Number(typeof o.seconds === "function" ? o.seconds() : o.seconds) || 5);
      active = true; last = -1; t0 = performance.now(); K.holds++;
      el.classList.add("holding"); fill.style.transition = "none";
      raf = requestAnimationFrame(tick);
    }
    function done() { K.holds = Math.max(0, K.holds - 1); if (!K.holds && K.pendingDraw) { K.pendingDraw = false; setTimeout(draw, 360); } }
    function drain() { el.classList.remove("holding"); fill.style.transition = "transform 300ms ease"; set(0); }
    function cancel() { if (!active) return; active = false; cancelAnimationFrame(raf); drain(); if (o.cancel) o.cancel(); done(); }
    function fire() {
      try { if (navigator.vibrate) navigator.vibrate(40); } catch (e) { /* not a phone */ }
      Promise.resolve().then(o.fire).catch(function () {});
      setTimeout(function () { drain(); if (o.cancel) o.cancel(); done(); }, 350);
    }
    el.addEventListener("pointerdown", function (e) {
      if (e.button) return;
      try { el.setPointerCapture(e.pointerId); } catch (x) { /* fine */ }
      start();
    });
    el.addEventListener("pointermove", function (e) {
      if (!active) return;
      var r = el.getBoundingClientRect();
      if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) cancel(); // slid off
    });
    el.addEventListener("pointerup", cancel);
    el.addEventListener("pointercancel", cancel);
    el.addEventListener("lostpointercapture", cancel);
    el.addEventListener("contextmenu", function (e) { e.preventDefault(); });
    el.addEventListener("keydown", function (e) { if ((e.key === " " || e.key === "Enter") && !e.repeat) { e.preventDefault(); start(); } else if (e.key === " " || e.key === "Enter") e.preventDefault(); });
    el.addEventListener("keyup", function (e) { if (e.key === " " || e.key === "Enter") cancel(); });
    el.addEventListener("blur", cancel);
  };

  // K.holdButton: a button that runs action(params) after a press-and-hold.
  // Idle: label + "HOLD 5 S · <sub>"; held: "Keep holding…" + "<VERB> IN 3 S".
  K.holdButton = function (label, sub, o) {
    var secs = Math.max(1, Number(o.seconds) || 5);
    var b = K.button(label, "", "hold " + (o.cls || ""));
    var lab = b.querySelector("span"), sm = K.el("small"); b.appendChild(sm);
    var idle = function () { lab.textContent = label; sm.textContent = "HOLD " + secs + " S" + (sub ? " · " + sub : ""); };
    idle();
    b.setAttribute("aria-label", label + ", press and hold " + secs + " seconds");
    K.hold(b, {
      seconds: secs, tone: o.tone,
      progress: function (left) { lab.textContent = o.holdLabel || "Keep holding…"; sm.textContent = (o.verb || "GOES") + " IN " + left + " S"; },
      cancel: idle,
      fire: function () { return K.act(o.action, o.params); },
    });
    return b;
  };

  K.press = function (b, name, params) {
    b.onclick = function () {
      if (K.editing || b.disabled) return;
      b.classList.add("busy");
      K.act(name, params).catch(function () {}).then(function () { b.classList.remove("busy"); });
    };
  };
  K.header = function (title, ctx) {
    var h = document.querySelector("header") || document.body.insertBefore(K.el("header"), document.body.firstChild);
    h.textContent = "";
    h.appendChild(K.el("h1", "", title));
    if (ctx) h.appendChild(K.el("span", "ctx", ctx));
  };
  K.empty = function (main, text) { main.textContent = ""; main.appendChild(K.el("div", "empty", text)); };

  function theme(t) { if (t) for (var k in t) if (k.indexOf("--") === 0) document.documentElement.style.setProperty(k, t[k]); }
  // A widget never redraws under a finger that is holding a button.
  function draw() { if (K.holds > 0) { K.pendingDraw = true; return; } document.body.classList.toggle("editing", K.editing); try { K.render(K.state, K.settings); } catch (e) { K.log("render: " + e.message); } }

  window.addEventListener("message", function (e) {
    if (e.source !== host) return;
    var m = e.data;
    if (!m || typeof m.t !== "string") return;
    switch (m.t) {
      case "init":
        K.state = m.state || {}; K.settings = m.settings || {}; K.size = m.size || K.size; K.editing = !!m.editing; K.touch = !!m.touch;
        K.stateAt = Date.now(); theme(m.theme); draw(); break;
      case "state": K.state = m.state || {}; K.stateAt = Date.now(); draw(); break;
      case "size": K.size = m.size || K.size; if (K.onSize) K.onSize(); else draw(); break;
      case "theme": theme(m.theme); break;
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

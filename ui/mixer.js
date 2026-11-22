// Audio mixer: one strip per chosen bus or input, on jadeck's shared
// frame kit fader strip (JK.Strip, the same strip as the WING faders):
// name, dB scale, fader (vMix volume), stereo LED meter with peak hold, a
// dB readout, MUTE (red when muted) and S (solo, when turned on). Levels
// come from the add-on's "meters" resource ~12 times a second; the
// widget's state is the fallback.
(function () {
  "use strict";
  var K = window.Kit, JK = window.JK, main = document.getElementById("main");
  var strips = [], byRef = {}, live = 0, rows = {}, built = "", raf = 0, started = false, shown = 0;

  function ids() {
    var l = K.list(K.settings.strips);
    if (l.length) return l;
    var ms = Array.isArray(K.state.mixerStrips) ? K.state.mixerStrips : [];
    if (ms.length) return ms.map(function (x) { return x.id; });
    var out = ["master"], a = K.state.audio;
    (a && Array.isArray(a.buses) ? a.buses : []).forEach(function (b) { out.push("bus" + b.id); });
    return out;
  }
  function fromState() {
    var by = {};
    (Array.isArray(K.state.mixerStrips) ? K.state.mixerStrips : []).forEach(function (x) { by[String(x.id).toLowerCase()] = x; });
    return ids().map(function (id) { var x = by[id.toLowerCase()]; return x ? Object.assign({ ref: id }, x) : null; }).filter(Boolean);
  }
  function opts() {
    return { meter: K.bool("meters", true), readout: K.bool("db", true), mute: K.bool("mute", true), solo: K.bool("solo", false), fader: K.bool("faders", true) };
  }
  function canWrite() { return !K.editing && K.state.connected !== false; }

  // build (re)makes the strips when the list, the settings or the layout change.
  function build(list) {
    var o = opts(), box = document.getElementById("strips");
    if (!box) { main.textContent = ""; box = K.el("div", "jk-strips"); box.id = "strips"; main.appendChild(box); }
    var r = main.getBoundingClientRect();
    var L = JK.stripLayout(r.width, r.height, list.length, { readout: o.readout, mute: o.mute, gap: 8 });
    shown = L.shown;
    var key = list.map(function (s) { return s.ref; }).join("|") + JSON.stringify(o) + L.rows + "x" + L.cols + ":" + L.shown + ":" + Math.round(L.rowH / 20);
    if (key === built) return;
    built = key;
    box.textContent = "";
    rows = {};
    JK.stripGrid(box, L);
    box.style.gap = "8px";
    list.slice(0, L.shown).forEach(function (s) {
      var ref = s.ref;
      rows[ref] = new JK.Strip(box, {
        width: L.colW, height: L.rowH, law: K.law, fader: o.fader, meter: o.meter, readout: o.readout, mute: o.mute,
        solo: o.solo && !!s.canSolo, canWrite: canWrite,
        onLevel: function (db) { return K.act("audio_volume", { target: ref, value: Math.round(K.dbVol(db)) }).catch(function () {}); },
        onMute: function () { var c = byRef[ref] || {}; return K.act("audio_mute", { target: ref, muted: !c.muted }).catch(function () {}); },
        onSolo: function () { var c = byRef[ref] || {}; return K.act("audio_solo", { target: ref, on: !c.solo }).catch(function () {}); },
      });
    });
  }

  function update(list) {
    strips = list;
    byRef = {};
    list.forEach(function (s) { byRef[s.ref] = s; });
    if (!list.length) {
      K.header("Audio", "");
      built = "";
      rows = {};
      return K.empty(main, K.state.connected === false ? (K.state.error || "vMix isn't answering") : "No audio strips. Pick some in this widget's settings.");
    }
    if (!built) K.header("Audio", ""); // the header's height first, then measure
    build(list);
    // strips that don't fit (each needs 56 px) are counted, not squeezed
    K.header("Audio", shown < list.length ? shown + " OF " + list.length + " STRIPS" : list.length + " STRIP" + (list.length === 1 ? "" : "S"));
    cancelAnimationFrame(raf);
    raf = requestAnimationFrame(paint);
  }
  function lvl(a) { var d = K.ampDb(a || 0); return d <= -144 ? null : d; }
  function paint() {
    Object.keys(rows).forEach(function (ref) {
      var s = byRef[ref];
      if (!s) return;
      var m = s.meter || [0, 0], db = K.volDb(s.volume || 0);
      rows[ref].set({
        tag: s.kind || "", name: s.name || s.label, stereo: true, db: db === -Infinity ? -144 : db,
        muted: !!s.muted, solo: !!s.solo, soloable: !!s.canSolo,
        l: s.muted ? null : lvl(m[0]), r: s.muted ? null : lvl(m.length > 1 ? m[1] : m[0]),
      });
    });
  }

  function poll() {
    K.resource("res:meters?ids=" + encodeURIComponent(ids().join(",")))
      .then(function (blob) { return blob.text(); })
      .then(function (t) { var j = JSON.parse(t); live = Date.now(); update(j.strips || []); })
      .catch(function () { if (!live || Date.now() - live > 3000) update(fromState()); })
      .then(function () { setTimeout(poll, K.bool("meters", true) ? 80 : 500); });
  }

  K.render = function () {
    // (build() compares a key of the list, settings and layout, so a state
    // update never rebuilds a strip under a finger)
    if (!live || Date.now() - live > 3000) update(fromState()); else update(strips);
    if (!started) { started = true; poll(); }
  };
  K.onSize = function () {
    if (!strips.length) return;
    build(strips);
    Object.keys(rows).forEach(function (ref) { rows[ref].fit(); });
    paint();
  };
  K.start();
})();

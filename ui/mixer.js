// Audio mixer: one strip per chosen bus or input, each with a fader (vMix
// volume, dB scale), a stereo LED meter with peak hold, a dB readout and
// a MUTE button (red when muted). Levels come from the add-on's "meters"
// resource ~12 times a second; the widget's state is the fallback.
(function () {
  "use strict";
  var K = window.Kit, main = document.getElementById("main");
  var strips = [], live = 0, hold = {}, meters = {}, built = "";
  var SCALE = [0, -5, -10, -20, -30, -40, -60];

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

  function build(list) {
    var showM = K.bool("meters", true), showDb = K.bool("db", true), showMute = K.bool("mute", true), showSolo = K.bool("solo", false), showF = K.bool("faders", true);
    var key = list.map(function (s) { return s.ref; }).join("|") + [showM, showDb, showMute, showSolo, showF].join("");
    var box = document.getElementById("strips");
    if (key === built && box) return box;
    built = key; meters = {};
    main.textContent = "";
    box = K.el("div"); box.id = "strips"; main.appendChild(box);
    list.forEach(function (s) {
      var st = K.el("section", "strip"); st.dataset.ref = s.ref;
      var who = K.el("div", "who"); who.appendChild(K.el("span", "cap", s.kind || "")); who.appendChild(K.el("b", "", s.name || s.label)); st.appendChild(who);
      var body = K.el("div", "body");
      if (showF) {
        var f = K.el("div", "fader live"); f.setAttribute("role", "slider"); f.setAttribute("aria-label", (s.name || s.label) + " volume");
        f.setAttribute("aria-valuemin", "0"); f.setAttribute("aria-valuemax", "100"); f.tabIndex = 0;
        var sc = K.el("div", "scale");
        SCALE.forEach(function (d) {
          var y = (100 - K.dbVol(d)) + "%";
          var tick = K.el("i", d === 0 ? "zero" : ""); tick.style.top = y; sc.appendChild(tick);
          var lbl = K.el("b", "", d === 0 ? "0" : String(d)); lbl.style.top = y; sc.appendChild(lbl);
        });
        f.appendChild(sc); f.appendChild(K.el("div", "track")); f.appendChild(K.el("div", "cap-m"));
        bindFader(f, s.ref); body.appendChild(f);
      }
      if (showM) {
        var m = K.el("div", "meter"), cv = K.el("canvas"); m.appendChild(cv);
        var lr = K.el("div", "lr"); lr.appendChild(K.el("span", "", "L")); lr.appendChild(K.el("span", "", "R")); m.appendChild(lr);
        body.appendChild(m);
        meters[s.ref] = { cv: cv, lvl: [0, 0], show: [0, 0], peak: [0, 0], peakAt: [0, 0] };
      }
      st.appendChild(body);
      if (showDb) st.appendChild(K.el("div", "ro", "−∞ dB"));
      var row = K.el("div", "row");
      if (showMute) { var mu = K.button("MUTE"); mu.classList.add("mute"); row.appendChild(mu); }
      if (showSolo && s.canSolo) { var so = K.button("SOLO"); so.classList.add("solo"); row.appendChild(so); }
      if (row.children.length) st.appendChild(row);
      box.appendChild(st);
    });
    return box;
  }

  // Strips keep a usable height: as few rows as fit, each strip at least
  // 64 px wide; short strips drop the scale, very short ones keep only the
  // name, the readout and MUTE.
  function layout(box, n) {
    var r = main.getBoundingClientRect(), gap = 8;
    var fitCols = Math.max(1, Math.floor((r.width + gap) / (64 + gap)));
    var rows = Math.ceil(n / Math.min(n, fitCols)), cols = Math.ceil(n / rows);
    var w = Math.min(150, (r.width - gap * (cols - 1)) / cols), h = (r.height - gap * (rows - 1)) / rows;
    box.style.gridTemplateColumns = "repeat(" + cols + ", " + Math.floor(w) + "px)";
    box.style.gridTemplateRows = "repeat(" + rows + ", minmax(0, 1fr))";
    box.querySelectorAll(".strip").forEach(function (s) { s.classList.toggle("compact", h < 230); s.classList.toggle("tiny", h < 150); s.classList.toggle("micro", h < 112); });
  }

  function update(list) {
    strips = list;
    K.header("Audio", list.length + " STRIP" + (list.length === 1 ? "" : "S"));
    if (!list.length) { built = ""; return K.empty(main, K.state.connected === false ? (K.state.error || "vMix isn't answering") : "No audio strips. Pick some in this widget's settings."); }
    var box = build(list);
    layout(box, list.length);
    list.forEach(function (s) {
      var st = box.querySelector('[data-ref="' + CSS.escape(s.ref) + '"]');
      if (!st) return;
      st.querySelector(".who .cap").textContent = s.kind || "";
      st.querySelector(".who b").textContent = s.name || s.label;
      var h = hold[s.ref], v = h && h.until > Date.now() ? h.v : (s.volume || 0);
      var f = st.querySelector(".fader");
      if (f) { f.querySelector(".cap-m").style.top = "calc(14px + (100% - 28px) * " + (1 - Math.max(0, Math.min(100, v)) / 100) + ")"; f.setAttribute("aria-valuenow", String(Math.round(v))); }
      var ro = st.querySelector(".ro"); if (ro) ro.textContent = K.dbText(K.volDb(v)) + " dB";
      st.classList.toggle("muted", !!s.muted);
      var mu = st.querySelector(".mute");
      if (mu) {
        mu.className = "btn live mute" + (s.muted ? " pgm" : "");
        mu.firstChild.textContent = s.muted ? "MUTED" : "MUTE";
        mu.setAttribute("aria-pressed", s.muted ? "true" : "false");
        K.press(mu, "audio_mute", { target: s.ref, muted: !s.muted });
      }
      var so = st.querySelector(".solo");
      if (so) { so.className = "btn live solo" + (s.solo ? " sel" : ""); K.press(so, "audio_solo", { target: s.ref, on: !s.solo }); }
      var m = meters[s.ref];
      if (m) { var a = s.meter || [0, 0]; m.lvl = [lvl(a[0]), lvl(a.length > 1 ? a[1] : a[0])]; if (s.muted) m.lvl = [0, 0]; }
    });
  }
  function lvl(a) { var d = K.ampDb(a || 0); return d <= -60 ? 0 : Math.min(1, (d + 60) / 60); }

  // LED meters on canvas: ~24-30 segments, green to 70 %, amber to 90 %, red above; peak hold 1.5 s.
  function paint() {
    var now = Date.now(), reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;
    Object.keys(meters).forEach(function (ref) {
      var m = meters[ref], cv = m.cv, dpr = window.devicePixelRatio || 1;
      var w = cv.clientWidth, h = cv.clientHeight;
      if (!w || !h) return;
      if (cv.width !== Math.round(w * dpr) || cv.height !== Math.round(h * dpr)) { cv.width = Math.round(w * dpr); cv.height = Math.round(h * dpr); }
      var g = cv.getContext("2d");
      g.setTransform(dpr, 0, 0, dpr, 0, 0);
      g.clearRect(0, 0, w, h);
      var segs = Math.max(12, Math.min(30, Math.floor(h / 7))), gap = 2, sh = (h - gap * (segs - 1)) / segs, cw = (w - 4) / 2;
      for (var c = 0; c < 2; c++) {
        var target = m.lvl[c];
        m.show[c] = reduce || target > m.show[c] ? target : Math.max(target, m.show[c] - 0.04);
        if (m.show[c] >= m.peak[c] || now - m.peakAt[c] > 1500) { m.peak[c] = m.show[c]; m.peakAt[c] = now; }
        var lit = Math.round(m.show[c] * segs), pk = Math.round(m.peak[c] * segs) - 1;
        for (var i = 0; i < segs; i++) {
          var frac = (i + 1) / segs, on = i < lit || (i === pk && pk > 0);
          g.fillStyle = on ? (frac > 0.9 ? "#e5484d" : frac > 0.7 ? "#f2a93b" : "#30a46c") : "#1C2128";
          g.fillRect(c * (cw + 4), h - (i + 1) * sh - i * gap, cw, sh);
        }
      }
    });
    requestAnimationFrame(paint);
  }

  // A fader sends at most every 150 ms while dragging, and always on release.
  function bindFader(f, ref) {
    var last = 0, timer = 0, drag = false, value = 0;
    function at(e) { var r = f.getBoundingClientRect(); var p = 1 - (e.clientY - r.top - 14) / Math.max(1, r.height - 28); return Math.round(Math.max(0, Math.min(1, p)) * 100); }
    function push(final) {
      clearTimeout(timer);
      var wait = 150 - (Date.now() - last);
      if (!final && wait > 0) { timer = setTimeout(function () { push(false); }, wait); return; }
      last = Date.now();
      K.act("audio_volume", { target: ref, value: value }).catch(function () {});
    }
    function move(e) { value = at(e); hold[ref] = { v: value, until: Date.now() + 1500 }; update(strips); push(false); }
    f.addEventListener("pointerdown", function (e) { if (K.editing) return; drag = true; f.classList.add("dragging"); f.setPointerCapture(e.pointerId); move(e); });
    f.addEventListener("pointermove", function (e) { if (drag) move(e); });
    function up() { if (!drag) return; drag = false; f.classList.remove("dragging"); hold[ref] = { v: value, until: Date.now() + 1500 }; push(true); }
    f.addEventListener("pointerup", up); f.addEventListener("pointercancel", up);
    f.addEventListener("keydown", function (e) {
      var c = strips.filter(function (s) { return s.ref === ref; })[0]; if (!c) return;
      var d = e.key === "ArrowUp" ? 2 : e.key === "ArrowDown" ? -2 : 0; if (!d) return;
      e.preventDefault(); value = Math.max(0, Math.min(100, Math.round(c.volume) + d)); hold[ref] = { v: value, until: Date.now() + 1500 }; update(strips); push(true);
    });
  }

  function poll() {
    K.resource("res:meters?ids=" + encodeURIComponent(ids().join(",")))
      .then(function (blob) { return blob.text(); })
      .then(function (t) { var j = JSON.parse(t); live = Date.now(); update(j.strips || []); })
      .catch(function () { if (!live || Date.now() - live > 3000) update(fromState()); })
      .then(function () { setTimeout(poll, K.bool("meters", true) ? 80 : 500); });
  }

  var started = false;
  K.render = function () {
    if (!live || Date.now() - live > 3000) update(fromState()); else update(strips);
    if (!started) { started = true; poll(); requestAnimationFrame(paint); }
  };
  K.onSize = function () { var b = document.getElementById("strips"); if (b) layout(b, strips.length); };
  K.start();
})();

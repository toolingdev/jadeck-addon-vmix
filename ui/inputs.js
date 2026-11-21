// Input grid: vMix's inputs as picture tiles (vMix has no thumbnail API,
// so tiles are neutral gradients). Red border = program, green = preview,
// a held cut fills the tile red. What a tap (or a hold) does
// comes from the add-on (the widget's "Tap" setting).
(function () {
  "use strict";
  var K = window.Kit, main = document.getElementById("main"), foot = null;
  var hues = { Capture: "#26324a", NDI: "#24382d", SRT: "#24382d", Stream: "#24382d", Video: "#352a45", VideoList: "#352a45", AudioFile: "#2f2a3a",
    GT: "#3a2a3e", Xaml: "#3a2a3e", Title: "#3a2a3e", Image: "#2a3445", Photos: "#2a3445", Colour: "#1d2026", Blank: "#1a1d22", Replay: "#3d2b2b" };
  var hints = { preview: "Tap = preview · take with Cut / Fade", preview_take: "Tap = preview · tap the green one again to cut",
    program_hold: "Press and hold = cut to program", program_confirm: "Press and hold = cut to program", program: "Tap = cut to program" };

  function fit(n, W, H, gap) {
    var best = { cols: 1, w: W, h: Math.max(20, H / n), score: -Infinity };
    for (var cols = 1; cols <= n; cols++) {
      var rows = Math.ceil(n / cols);
      var w = (W - gap * (cols - 1)) / cols, h = (H - gap * (rows - 1)) / rows;
      if (w <= 0 || h <= 0) continue;
      var th = Math.min(h, w * 0.75), tw = Math.min(w, th * 16 / 9 * 1.15);
      var score = tw * th - (th < 44 ? 1e7 : 0) - (tw < 60 ? 1e7 : 0);
      if (score > best.score) best = { cols: cols, w: tw, h: th, score: score };
    }
    return best;
  }

  K.render = function (st, set) {
    var items = Array.isArray(st.inputGrid) ? st.inputGrid : [];
    var mix = Number(set.mix || 1) || 1;
    K.header("Inputs", mix > 1 ? "MIX " + mix : "MIX 1");
    if (st.connected === false) return K.empty(main, st.error || "vMix isn't answering");
    if (!items.length) return K.empty(main, "No inputs. Pick some in this widget's settings.");
    var numbers = K.bool("numbers", true);
    var r = main.getBoundingClientRect(), gap = r.width < 360 ? 6 : 10;
    // Decide from the whole frame, not from main: showing the footer shrinks
    // main, and judging by main made the hint flicker on and off forever.
    var tall = (window.innerHeight || document.body.clientHeight) > 260;
    if (!foot && tall) { foot = K.el("footer"); document.body.appendChild(foot); }
    if (foot) { foot.hidden = !tall; foot.textContent = (hints[set.tap] || hints.preview) + " · inputs tracked by vMix ID"; r = main.getBoundingClientRect(); }
    var f = fit(items.length, r.width, r.height, gap);
    var grid = document.getElementById("grid");
    if (!grid) { main.textContent = ""; grid = K.el("div"); grid.id = "grid"; main.appendChild(grid); }
    grid.style.gap = gap + "px";
    grid.style.gridTemplateColumns = "repeat(" + f.cols + ", " + Math.floor(f.w) + "px)";
    grid.style.gridAutoRows = Math.floor(f.h) + "px";
    while (grid.children.length > items.length) grid.removeChild(grid.lastChild);
    items.forEach(function (it, i) {
      var t = grid.children[i];
      if (!t) { t = K.el("div", "tile live"); t.setAttribute("role", "button"); t.tabIndex = 0; grid.appendChild(t); }
      var tone = it.tone === "pgm" || it.tone === "pvw" ? it.tone : "";
      t.className = "tile live" + (tone ? " " + tone : "") + (t.classList.contains("holding") ? " holding" : "") + (f.h < 90 ? " small" : "") + (f.h < 60 ? " tiny" : "");
      t.style.setProperty("--a", hues[it.type] || "#2a3039");
      Array.prototype.slice.call(t.childNodes).forEach(function (n) { if (!n.classList || !n.classList.contains("hold-fill")) t.removeChild(n); });
      var inline = f.h < 60;
      if (numbers && it.number != null && !inline) t.appendChild(K.el("span", "tag", String(it.number)));
      var pills = K.el("div", "pills");
      if (it.state === "Running" && it.remaining > 0) pills.appendChild(K.el("span", "time", "▶ " + K.clock(it.remaining)));
      if (tone) pills.appendChild(K.el("span", "pill " + tone, tone === "pgm" ? "PGM" : "PVW"));
      t.appendChild(pills);
      t.appendChild(K.el("div", "name", (inline && numbers && it.number != null ? it.number + " · " : "") + (it.title || it.label || "")));
      t.setAttribute("aria-label", (it.label || it.title) + (tone === "pgm" ? ", program" : tone === "pvw" ? ", preview" : ""));
      t._item = it;
      if (it.hold) {
        // Held cut: the tile fills red, "Keep holding… / CUTS IN 3 S".
        t.onclick = null;
        if (!t._holdBound) {
          t._holdBound = true;
          var ask = null;
          K.hold(t, {
            seconds: function () { return t._item.hold; },
            progress: function (left) {
              if (!ask) { ask = K.el("div", "ask"); t.appendChild(ask); }
              ask.textContent = "";
              ask.appendChild(K.el("b", "", t._item.holdLabel || "Keep holding…"));
              ask.appendChild(K.el("small", "", "CUTS IN " + left + " S"));
            },
            cancel: function () { if (ask) { ask.remove(); ask = null; } },
            fire: function () { return K.act(t._item.action, t._item.params || {}); },
          });
        }
        t.setAttribute("aria-label", t.getAttribute("aria-label") + ", press and hold " + it.hold + " seconds to cut");
      } else {
        t.onclick = function () { if (!K.editing && t._item.action && !t._holdBound) K.act(t._item.action, t._item.params || {}).catch(function () {}); };
      }
      t.onkeydown = function (e) { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); t.onclick(); } };
    });
  };
  K.onSize = function () { K.render(K.state, K.settings); };
  K.start();
})();

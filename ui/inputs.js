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
  var MENU_MS = 550; // press this long on a tile: the overlay menu
  var menu = null;   // the open overlay menu: {input, el}

  // Overlay channels this input is on (on air or in the preview overlay).
  function overlaysOf(st, number) {
    var on = {};
    (Array.isArray(st.overlays) ? st.overlays : []).forEach(function (o) {
      if (o && o.input === number) on[o.number] = o.preview ? "pvw" : "pgm";
    });
    return on;
  }
  function closeMenu() { if (menu) { menu.el.remove(); menu = null; } }
  // The overlay menu: tap a number to put this input on that overlay live;
  // the red one (already on air there) takes it off.
  function drawMenu(st) {
    if (!menu) return;
    var it = menu.item, count = Number(K.settings.overlayCount || 4) === 8 ? 8 : 4;
    var on = overlaysOf(st, it.number), box = menu.el;
    box.textContent = "";
    var card = K.el("div", "ov-card");
    card.setAttribute("role", "dialog");
    card.setAttribute("aria-label", (it.title || it.label) + " to an overlay");
    card.appendChild(K.el("div", "ov-title", (it.title || it.label || "Input") + " → overlay"));
    var grid = K.el("div", "ov-grid" + (count === 8 ? " eight" : ""));
    for (var n = 1; n <= count; n++) (function (n) {
      var state = on[n];
      var b = window.JK.button({ label: String(n), sub: state === "pgm" ? "ON AIR" : state === "pvw" ? "PREVIEW" : "",
        tone: state === "pgm" ? "pgm" : state === "pvw" ? "pvw" : "",
        title: state === "pgm" ? "Take it off overlay " + n : "Put it on overlay " + n,
        onClick: function () {
          var p = { n: n, op: state === "pgm" ? "out" : "in" };
          if (state !== "pgm") p.input = (it.params && it.params.input) || it.key || it.number;
          return K.act("overlay", p).then(function () { closeMenu(); }, function () {});
        } });
      grid.appendChild(b);
    })(n);
    card.appendChild(grid);
    if (box.clientHeight > 230) card.appendChild(K.el("p", "ov-help", "Tap a number: puts this input on that overlay live (tap the red one to take it off)."));
    var close = window.JK.button({ label: "Close", onClick: function () { closeMenu(); } });
    close.classList.add("ov-close");
    card.appendChild(close);
    box.appendChild(card);
  }
  function openMenu(it) {
    closeMenu();
    var box = K.el("div", "ov-menu");
    box.onclick = function (e) { if (e.target === box) closeMenu(); };
    document.body.appendChild(box);
    menu = { item: it, el: box };
    drawMenu(K.state);
  }
  document.addEventListener("keydown", function (e) { if (e.key === "Escape") closeMenu(); });

  // Press and hold a tile (no hold-to-cut on it) to open the overlay menu.
  // A held press never also previews.
  function bindMenuPress(t) {
    var timer = 0, fired = false, x = 0, y = 0;
    function clear() { clearTimeout(timer); timer = 0; t.classList.remove("pressing"); }
    t.addEventListener("pointerdown", function (e) {
      fired = false;
      if (K.editing || !K.bool("overlayMenu", true) || t._item.hold) return;
      x = e.clientX; y = e.clientY;
      t.classList.add("pressing");
      timer = setTimeout(function () { timer = 0; fired = true; t.classList.remove("pressing"); openMenu(t._item); }, MENU_MS);
    });
    t.addEventListener("pointermove", function (e) { if (timer && Math.abs(e.clientX - x) + Math.abs(e.clientY - y) > 12) clear(); });
    t.addEventListener("pointerup", clear);
    t.addEventListener("pointercancel", clear);
    t.addEventListener("pointerleave", clear);
    t.addEventListener("contextmenu", function (e) {
      e.preventDefault();
      if (!K.editing && K.bool("overlayMenu", true)) { fired = true; openMenu(t._item); }
    });
    t._menuFired = function () { var f = fired; fired = false; return f; };
  }

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

  // Touch screens: a tile is never smaller than a finger (32 px). When
  // all inputs can't fit that big, they go in pages and the last tile
  // turns the page ("› 2/3").
  var page = 0, pager = null, MIN_TAP = 32;
  var coarse = !!(window.matchMedia && window.matchMedia("(pointer: coarse)").matches);

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
    var menuOn = K.bool("overlayMenu", true);
    var hint = (hints[set.tap] || hints.preview) + (menuOn ? (set.tap === "program_hold" || set.tap === "program_confirm" ? " · right-click or ◧ = overlay menu" : " · hold = overlay menu") : "");
    if (foot) { foot.hidden = !tall; foot.textContent = hint; r = main.getBoundingClientRect(); }
    var f = fit(items.length, r.width, r.height, gap), pages = 1;
    if (coarse && items.length > 1 && (f.w < MIN_TAP + 2 || f.h < MIN_TAP)) {
      var per = items.length - 1;
      for (; per > 1; per--) {
        f = fit(per + 1, r.width, r.height, gap);
        if (f.w >= MIN_TAP + 2 && f.h >= MIN_TAP) break;
      }
      if (per <= 1) f = fit(2, r.width, r.height, gap);
      per = Math.max(1, per);
      pages = Math.ceil(items.length / per);
      page = page % pages;
      items = items.slice(page * per, page * per + per);
    } else page = 0;
    var grid = document.getElementById("grid");
    if (!grid) { main.textContent = ""; grid = K.el("div"); grid.id = "grid"; main.appendChild(grid); }
    grid.style.gap = gap + "px";
    grid.style.gridTemplateColumns = "repeat(" + f.cols + ", " + Math.floor(f.w) + "px)";
    grid.style.gridAutoRows = Math.floor(f.h) + "px";
    if (pager && pager.parentNode) pager.parentNode.removeChild(pager);
    while (grid.children.length > items.length) grid.removeChild(grid.lastChild);
    items.forEach(function (it, i) {
      var t = grid.children[i];
      if (!t) { t = K.el("div", "tile jk-live"); t.setAttribute("role", "button"); t.tabIndex = 0; grid.appendChild(t); bindMenuPress(t); }
      var tone = it.tone === "pgm" || it.tone === "pvw" ? it.tone : "";
      t.className = "tile jk-live" + (tone ? " " + tone : "") + (t._holdBound ? " jk-hold" : "") + (t.classList.contains("jk-holding") ? " jk-holding" : "") +
        (f.h < 90 ? " small" : "") + (f.h < 60 ? " tiny" : "");
      t.style.setProperty("--a", hues[it.type] || "#2a3039");
      Array.prototype.slice.call(t.childNodes).forEach(function (n) { if (!n.classList || !n.classList.contains("jk-hold-fill")) t.removeChild(n); });
      var inline = f.h < 60;
      if (numbers && it.number != null && !inline) t.appendChild(K.el("span", "tag", String(it.number)));
      var pills = K.el("div", "pills");
      if (it.state === "Running" && it.remaining > 0) pills.appendChild(K.el("span", "time", "▶ " + K.clock(it.remaining)));
      if (tone) pills.appendChild(K.el("span", "jk-tag " + tone, tone === "pgm" ? "PGM" : "PVW"));
      var ovOn = overlaysOf(st, it.number), ovN = Object.keys(ovOn).filter(function (k) { return ovOn[k] === "pgm"; });
      if (ovN.length && !inline) pills.appendChild(K.el("span", "jk-tag ov", "OV " + ovN.join(",")));
      t.appendChild(pills);
      if (it.hold && menuOn && !inline) {
        // Hold-to-cut tiles keep the press for the cut: a small corner button opens the menu.
        var ob = K.el("button", "ov-open", "◧");
        ob.type = "button"; ob.title = "Overlay menu"; ob.setAttribute("aria-label", "Overlay menu");
        ob.addEventListener("pointerdown", function (e) { e.stopPropagation(); });
        ob.onclick = function (e) { e.stopPropagation(); if (!K.editing) openMenu(t._item); };
        t.appendChild(ob);
      }
      t.appendChild(K.el("div", "name", (inline && numbers && it.number != null ? it.number + " · " : "") + (it.title || it.label || "")));
      t.setAttribute("aria-label", (it.label || it.title) + (tone === "pgm" ? ", program" : tone === "pvw" ? ", preview" : ""));
      t._item = it;
      if (it.hold) {
        // Held cut: the tile fills red, "Keep holding… / CUTS IN 3 S".
        t.onclick = null;
        if (!t._holdBound) {
          t._holdBound = true;
          var ask = null;
          window.JK.hold(t, { // the shared frame kit's hold to confirm
            seconds: function () { return t._item.hold; },
            onProgress: function (left) {
              if (!ask) { ask = K.el("div", "ask"); t.appendChild(ask); }
              ask.textContent = "";
              ask.appendChild(K.el("b", "", t._item.holdLabel || "Keep holding…"));
              ask.appendChild(K.el("small", "", "CUTS IN " + left + " S"));
            },
            onCancel: function () { if (ask) { ask.remove(); ask = null; } },
            onFire: function () { return K.act(t._item.action, t._item.params || {}); },
          });
        }
        t.setAttribute("aria-label", t.getAttribute("aria-label") + ", press and hold " + it.hold + " seconds to cut");
      } else {
        t.onclick = function () {
          if (t._menuFired && t._menuFired()) return; // that press opened the overlay menu
          if (!K.editing && t._item.action && !t._holdBound) K.act(t._item.action, t._item.params || {}).catch(function () {});
        };
      }
      t.onkeydown = function (e) {
        if ((e.key === "Enter" || e.key === " ") && t.onclick) { e.preventDefault(); t.onclick(); }
        else if ((e.key === "o" || e.key === "O" || e.key === "ContextMenu") && menuOn && !K.editing) { e.preventDefault(); openMenu(t._item); }
      };
    });
    if (pages > 1) {
      if (!pager) {
        pager = K.el("button", "tile pager");
        pager.type = "button";
        pager.onclick = function () { if (!K.editing) { page++; K.render(K.state, K.settings); } };
      }
      pager.textContent = "";
      pager.appendChild(K.el("b", "", "›"));
      pager.appendChild(K.el("small", "", (page + 1) + "/" + pages));
      pager.setAttribute("aria-label", "More inputs, page " + (page + 1) + " of " + pages);
      grid.appendChild(pager);
    }
    if (menu) {
      // keep the open menu pointing at the same input, with live overlay state
      var still = items.filter(function (x) { return x.number === menu.item.number; })[0];
      if (still && !K.editing) { menu.item = still; drawMenu(st); } else closeMenu();
    }
  };
  K.onSize = function () { K.render(K.state, K.settings); };
  K.start();
})();

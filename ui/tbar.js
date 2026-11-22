// T-bar as a fader: drag from the bottom (preview) to the top (program).
// vMix finishes the transition when the bar reaches the end; the cap
// then goes back to the start. Upright when the box is tall, sideways
// when it is wide. Sends at most every 100 ms while dragging.
(function () {
  "use strict";
  var K = window.Kit, main = document.getElementById("main"), local = null, until = 0;
  var bar, fader, cap, ro, wide = false, drag = false, last = 0, timer = 0;

  function build() {
    main.textContent = "";
    bar = K.el("div"); bar.id = "bar";
    var well = K.el("div", "well");
    fader = K.el("div", "fader jk-live"); fader.setAttribute("role", "slider"); fader.setAttribute("aria-label", "T-bar"); fader.tabIndex = 0;
    var sc = K.el("div", "scale");
    for (var i = 0; i <= 10; i++) { var t = K.el("i", i === 0 || i === 10 ? "zero" : ""); t.style.top = (i * 10) + "%"; sc.appendChild(t); }
    fader.appendChild(sc); fader.appendChild(K.el("div", "track")); cap = K.el("div", "cap-m"); fader.appendChild(cap);
    well.appendChild(fader); bar.appendChild(well);
    ro = K.el("div", "jk-ro", "0%"); bar.appendChild(ro);
    main.appendChild(bar);
    fader.addEventListener("pointerdown", function (e) { if (K.editing) return; drag = true; fader.classList.add("dragging"); fader.setPointerCapture(e.pointerId); move(e); });
    fader.addEventListener("pointermove", function (e) { if (drag) move(e); });
    fader.addEventListener("pointerup", up); fader.addEventListener("pointercancel", up);
  }
  function at(e) {
    var r = fader.getBoundingClientRect();
    var p = wide ? (e.clientX - r.left - 14) / Math.max(1, r.width - 28) : 1 - (e.clientY - r.top - 14) / Math.max(1, r.height - 28);
    return Math.max(0, Math.min(1, p));
  }
  function send(final) {
    clearTimeout(timer);
    var wait = 100 - (Date.now() - last);
    if (!final && wait > 0) { timer = setTimeout(function () { send(false); }, wait); return; }
    last = Date.now();
    K.act("set_tbar", { value: Math.round(local * 1000) / 1000 }).catch(function () {});
  }
  function move(e) { local = at(e); until = Date.now() + 1500; draw(); send(false); }
  function up() {
    if (!drag) return;
    drag = false; fader.classList.remove("dragging"); send(true);
    if (local >= 0.999) { local = 0; } // vMix completed the transition
    until = Date.now() + 1500; draw();
  }
  function draw() {
    if (!bar) build();
    var r = main.getBoundingClientRect();
    wide = r.width > r.height * 1.3;
    bar.classList.toggle("wide", wide);
    var v = local !== null && (drag || Date.now() < until) ? local : Number(K.state.tbar) || 0;
    cap.style.top = wide ? "50%" : "calc(14px + (100% - 28px) * " + (1 - v) + ")";
    cap.style.left = wide ? "calc(14px + (100% - 28px) * " + v + ")" : "50%";
    ro.textContent = Math.round(v * 100) + "%";
    fader.setAttribute("aria-valuenow", String(Math.round(v * 100)));
  }
  K.render = function () { K.header("T-bar", "MAIN MIX"); if (K.state.connected === false) { bar = null; return K.empty(main, K.state.error || "vMix isn't answering"); } draw(); };
  K.onSize = function () { if (bar) draw(); };
  K.start();
})();

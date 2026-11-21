// Outputs: streaming, recording, external, MultiCorder… one row each with
// a status dot (red, pulsing while live), how long it has run, and
// Start / Stop, pressed and held (the widget's holdSeconds, default 5 s).
(function () {
  "use strict";
  var K = window.Kit, main = document.getElementById("main"), tick = 0;

  function elapsed(r) {
    if (!r.on) return "--:--:--";
    if (r.id === "record") return K.hms(((r.seconds || 0) * 1000) + (Date.now() - K.stateAt));
    if (r.since) return K.hms(Date.now() - Date.parse(r.since));
    return "ON";
  }
  // Risky outputs are pressed and held; fullscreen / playlist are a tap.
  function button(r, label, sub, short) {
    var b;
    if (r.hold) {
      b = K.holdButton(label, sub, { seconds: r.hold, action: "output", params: r.params, verb: r.on ? "STOPS" : "STARTS", cls: (r.on ? "armedlive" : "") + (short ? " center" : "") });
    } else {
      b = K.button(label, sub, r.on ? "pgm" : "");
      K.press(b, "output", r.params);
    }
    return b;
  }
  K.render = function (st) {
    K.header("Outputs", st.recording === undefined ? "" : "");
    var rows = Array.isArray(st.outputRows) ? st.outputRows : [];
    if (st.connected === false) return K.empty(main, st.error || "vMix isn't answering");
    if (!rows.length) return K.empty(main, "Pick outputs in this widget's settings.");
    main.textContent = "";
    var box = K.el("div"); box.id = "rows";
    box.classList.toggle("narrow", main.clientWidth < 240);
    var tap = K.touch ? 48 : 40;
    if (main.clientHeight < rows.length * (tap + 6) || main.clientWidth < 190) {
      // Too little room for a list: one button per output, name and time on it.
      var per = Math.max(1, Math.floor((main.clientHeight + 6) / (tap + 6))), cols = Math.ceil(rows.length / per);
      box.className = "tight"; box.style.gridTemplateColumns = "repeat(" + cols + ", minmax(0, 1fr))";
      rows.forEach(function (r) {
        var b = button(r, (r.on ? "Stop " : "Start ") + r.name, r.on ? "LIVE" : "OFF AIR", false);
        box.appendChild(b);
      });
      main.appendChild(box);
      return;
    }
    rows.forEach(function (r) {
      var row = K.el("div", "orow" + (r.on ? " on" : ""));
      row.appendChild(K.el("span", "dot" + (r.on ? (r.id === "stream" || r.id === "record" ? " live" : " on") : "")));
      row.appendChild(K.el("span", "nm", r.name));
      var t = K.el("span", "ro", elapsed(r)); t.dataset.id = r.id; row.appendChild(t);
      var b = button(r, r.on ? "Stop" : "Start", "", true);
      row.appendChild(b);
      box.appendChild(row);
    });
    main.appendChild(box);
    if (!tick) tick = setInterval(function () {
      (K.state.outputRows || []).forEach(function (r) { var e = main.querySelector('[data-id="' + r.id + '"]'); if (e) e.textContent = elapsed(r); });
    }, 500);
  };
  K.start();
})();

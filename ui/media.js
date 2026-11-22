// Media: one video / audio / list input (the widget's "Input" setting):
// a progress bar with position and length (counting on between state
// updates while it plays), and Restart, Play, Pause, Next, Loop (the
// shared frame kit's buttons; amber = the current one).
(function () {
  "use strict";
  var K = window.Kit, main = document.getElementById("main"), timer = 0;

  function pos(m) { return Math.min(m.duration || 0, (m.position || 0) + (m.state === "Running" ? Date.now() - K.stateAt : 0)); }
  function tickBar() {
    var m = K.state.media; if (!m) return;
    var p = pos(m), bar = main.querySelector(".prog i"), t = main.querySelector(".times span");
    if (bar) bar.style.width = (m.duration > 0 ? (p / m.duration) * 100 : 0) + "%";
    if (t) t.textContent = K.clock(p);
  }
  K.render = function (st) {
    var m = st.media;
    K.header(m ? "Media · " + m.title : "Media", m ? "INPUT " + m.number : "");
    if (st.connected === false) return K.empty(main, st.error || "vMix isn't answering");
    if (!m) return K.empty(main, "No video, audio or list input in vMix.");
    main.textContent = "";
    var box = K.el("div"); box.id = "media";
    var prog = K.el("div", "prog"); prog.appendChild(K.el("i")); box.appendChild(prog);
    var times = K.el("div", "times"); times.appendChild(K.el("span", "", K.clock(pos(m)))); times.appendChild(K.el("span", "", m.duration > 0 ? K.clock(m.duration) : "live")); box.appendChild(times);
    if (m.item) box.appendChild(K.el("div", "item", m.item));
    var playing = m.state === "Running", c = K.el("div", "ctrls");
    var defs = [
      ["⟲", "restart", "Restart", false], ["▶", "play", "Play", !playing], ["❚❚", "pause", "Pause", playing],
      ["⏭", "next", "Next item", false], ["⟳", m.loop ? "loop_off" : "loop_on", m.loop ? "Loop on" : "Loop off", !!m.loop],
    ];
    defs.forEach(function (d) {
      var b = window.JK.button({ label: d[0], title: d[2], active: d[3], disabled: d[1] === "next" && !m.list });
      b.setAttribute("aria-label", d[2]);
      K.press(b, "media", { input: m.key, op: d[1] });
      c.appendChild(b);
    });
    box.appendChild(c);
    main.appendChild(box);
    tickBar();
    if (!timer) timer = setInterval(tickBar, 250);
  };
  K.start();
})();

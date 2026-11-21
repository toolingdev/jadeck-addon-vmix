package main

// A fake vMix for the tests: the Web Controller (HTTP, Basic auth,
// vMix's error texts) and the TCP API (VERSION greeting, CRLF lines,
// OK / ER / length-prefixed replies, SUBSCRIBE TALLY / ACTS pushes,
// PING), both able to act like "Restrict access to LAN only" (accept,
// then close). Behaviour follows VMIX-API.md (vMix 29.0.0.50).

import (
	"bufio"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type fInput struct {
	key, title, typ, state string
	audio                  bool
	muted                  bool
	volume                 int
	fields                 [][2]string // name, value (text)
	list                   []string
	selected               int
	duration, position     int
	loop                   bool
}

type fakeVmix struct {
	t *testing.T

	mu          sync.Mutex
	inputs      []*fInput
	program     int
	preview     int
	mixes       map[int][2]int // mix number (2-16) → program, preview
	overlays    [17]int
	ftb         bool
	recording   bool
	recSeconds  int
	streaming   bool
	external    bool
	multicorder bool
	masterVol   int
	masterMuted bool
	busMuted    map[string]bool
	fader       int
	calls       []string // "http Name?query" / "tcp Name?query"
	xmlReads    int
	password    string
	reject      bool // LAN-only: accept, then close
	noPong      bool
	unknownFns  map[string]bool // names this vMix answers "No suitable Function"

	httpLn, tcpLn net.Listener
	httpSrv       *http.Server
	tcpConns      map[net.Conn]bool
	subs          map[net.Conn]*fSub
}

type fSub struct {
	tally, acts bool
	wmu         *sync.Mutex
}

func newFake(t *testing.T) *fakeVmix {
	f := &fakeVmix{t: t, program: 1, preview: 2, mixes: map[int][2]int{2: {3, 1}}, masterVol: 100,
		busMuted: map[string]bool{}, unknownFns: map[string]bool{"omtpreviewon": true},
		tcpConns: map[net.Conn]bool{}, subs: map[net.Conn]*fSub{}}
	f.inputs = []*fInput{
		{key: "aaaaaaaa-0000-4000-8000-000000000001", title: "Camera 1", typ: "Capture", state: "Running", audio: true, volume: 100},
		{key: "aaaaaaaa-0000-4000-8000-000000000002", title: "Camera 2", typ: "Capture", state: "Running", audio: true, volume: 80},
		{key: "aaaaaaaa-0000-4000-8000-000000000003", title: "Clip", typ: "Video", state: "Paused", audio: true, volume: 90, duration: 60000, position: 1000},
		{key: "aaaaaaaa-0000-4000-8000-000000000004", title: "Lower third", typ: "GT", state: "Running",
			fields: [][2]string{{"Name.Text", "Jane"}, {"Role.Text", "Speaker"}}},
		{key: "aaaaaaaa-0000-4000-8000-000000000005", title: "Music", typ: "VideoList", state: "Paused", audio: true, volume: 70,
			list: []string{`C:\m\a.mp3`, `C:\m\b.mp3`}, selected: 1, duration: 1000},
	}
	var err error
	if f.httpLn, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	if f.tcpLn, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	f.httpSrv = &http.Server{Handler: http.HandlerFunc(f.serveHTTP)}
	go f.httpSrv.Serve(&rejectListener{Listener: f.httpLn, f: f})
	go f.acceptTCP()
	t.Cleanup(f.close)
	return f
}

func (f *fakeVmix) close() {
	f.httpSrv.Close()
	f.tcpLn.Close()
	f.mu.Lock()
	for c := range f.tcpConns {
		c.Close()
	}
	f.mu.Unlock()
}

func (f *fakeVmix) httpPort() int { return f.httpLn.Addr().(*net.TCPAddr).Port }
func (f *fakeVmix) tcpPort() int  { return f.tcpLn.Addr().(*net.TCPAddr).Port }

func (f *fakeVmix) config(extra map[string]any) map[string]any {
	c := map[string]any{"host": "127.0.0.1", "port": f.httpPort(), "tcpPort": f.tcpPort()}
	for k, v := range extra {
		c[k] = v
	}
	return c
}

func (f *fakeVmix) set(fn func(f *fakeVmix)) {
	f.mu.Lock()
	fn(f)
	f.mu.Unlock()
}

func (f *fakeVmix) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeVmix) clearCalls() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

// dropTCP closes every TCP API client (vMix restarting, a cable pulled).
func (f *fakeVmix) dropTCP() {
	f.mu.Lock()
	for c := range f.tcpConns {
		c.Close()
	}
	f.mu.Unlock()
}

type rejectListener struct {
	net.Listener
	f *fakeVmix
}

func (l *rejectListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		l.f.mu.Lock()
		rej := l.f.reject
		l.f.mu.Unlock()
		if !rej {
			return c, nil
		}
		c.Close()
	}
}

// ---------------------------------------------------------------- XML

func b2s(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

func (f *fakeVmix) xmlLocked() string {
	var b strings.Builder
	b.WriteString("<vmix>\r\n<version>29.0.0.50</version>\r\n<edition>4K</edition>\r\n<inputs>\r\n")
	for i, in := range f.inputs {
		fmt.Fprintf(&b, `<input key="%s" number="%d" type="%s" title="%s" shortTitle="%s" state="%s" position="%d" duration="%d" loop="%s"`,
			in.key, i+1, in.typ, html.EscapeString(in.title), html.EscapeString(in.title), in.state, in.position, in.duration, b2s(in.loop))
		if in.audio {
			fmt.Fprintf(&b, ` muted="%s" volume="%d" balance="0" solo="False" soloPFL="False" audiobusses="M" meterF1="0.5" meterF2="0.25" gainDb="0"`, b2s(in.muted), in.volume)
		}
		if len(in.list) > 0 {
			fmt.Fprintf(&b, ` selectedIndex="%d"`, in.selected)
		}
		b.WriteString(">" + html.EscapeString(in.title))
		for j, fl := range in.fields {
			fmt.Fprintf(&b, `<text index="%d" name="%s">%s</text>`, j, fl[0], html.EscapeString(fl[1]))
		}
		if len(in.list) > 0 {
			b.WriteString("<list>")
			for j, it := range in.list {
				sel := ""
				if j+1 == in.selected {
					sel = ` selected="true"`
				}
				fmt.Fprintf(&b, "<item%s>%s</item>", sel, html.EscapeString(it))
			}
			b.WriteString("</list>")
		}
		b.WriteString("</input>\r\n")
	}
	b.WriteString("</inputs>\r\n<overlays>")
	for n := 1; n <= 16; n++ {
		if f.overlays[n] != 0 {
			fmt.Fprintf(&b, `<overlay number="%d">%d</overlay>`, n, f.overlays[n])
		} else {
			fmt.Fprintf(&b, `<overlay number="%d" />`, n)
		}
	}
	fmt.Fprintf(&b, "</overlays>\r\n<preview>%d</preview>\r\n<active>%d</active>\r\n<fadeToBlack>%s</fadeToBlack>\r\n", f.preview, f.program, b2s(f.ftb))
	b.WriteString(`<transitions><transition number="1" effect="Fade" duration="500" /><transition number="2" effect="Merge" duration="1000" /><transition number="3" effect="Wipe" duration="1000" /><transition number="4" effect="CubeZoom" duration="1000" /></transitions>` + "\r\n")
	if f.recording {
		fmt.Fprintf(&b, `<recording duration="%d" filename1="C:\rec\a.mp4">True</recording>`, f.recSeconds)
	} else {
		b.WriteString("<recording>False</recording>")
	}
	fmt.Fprintf(&b, "<external>%s</external>", b2s(f.external))
	if f.streaming {
		b.WriteString(`<streaming channel1="True" channel3="True">True</streaming>`)
	} else {
		b.WriteString("<streaming>False</streaming>")
	}
	fmt.Fprintf(&b, "<playList>False</playList><multiCorder>%s</multiCorder><fullscreen>False</fullscreen>\r\n", b2s(f.multicorder))
	for n := 2; n <= 16; n++ {
		if m, ok := f.mixes[n]; ok {
			fmt.Fprintf(&b, `<mix number="%d"><preview>%d</preview><active>%d</active></mix>`, n, m[1], m[0])
		}
	}
	fmt.Fprintf(&b, "\r\n<audio><master volume=\"%d\" muted=\"%s\" meterF1=\"0.5\" meterF2=\"0.5\" headphonesVolume=\"74\" />", f.masterVol, b2s(f.masterMuted))
	for _, bus := range []string{"A", "B"} {
		fmt.Fprintf(&b, `<bus%s volume="80" muted="%s" meterF1="0.1" meterF2="0.1" solo="False" sendToMaster="False" />`, bus, b2s(f.busMuted[bus]))
	}
	b.WriteString("</audio>\r\n<dynamic><input1></input1><value1></value1></dynamic>\r\n</vmix>")
	return b.String()
}

func (f *fakeVmix) tallyLocked() string {
	var b strings.Builder
	for i := range f.inputs {
		switch i + 1 {
		case f.program:
			b.WriteByte('1')
		case f.preview:
			b.WriteByte('2')
		default:
			b.WriteByte('0')
		}
	}
	return b.String()
}

// ----------------------------------------------------------- functions

// resolveLocked finds an input like ShortcutInput.Parse: number, key or title.
func (f *fakeVmix) resolveLocked(s string) int {
	if s == "" || s == "0" {
		return f.preview
	}
	if s == "-1" {
		return f.program
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	for i, in := range f.inputs {
		if strings.EqualFold(in.key, s) || in.title == s {
			return i + 1
		}
	}
	return 0
}

// run executes a function; ok=false means "No suitable Function".
func (f *fakeVmix) run(via, name string, q url.Values) (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lname := strings.ToLower(name)
	if lname == "scriptstartdynamic" {
		return false, "Function not supported over API"
	}
	if _, known := functions[lname]; !known || f.unknownFns[lname] {
		return false, "No suitable Function could be found."
	}
	qq := url.Values{}
	for k, v := range q {
		if k != "Function" {
			qq[k] = v
		}
	}
	f.calls = append(f.calls, via+" "+name+"?"+qq.Encode())
	in := f.resolveLocked(q.Get("Input"))
	mix := 0
	if m := q.Get("Mix"); m != "" {
		mix, _ = strconv.Atoi(m)
	}
	setPgm := func(n int) {
		if mix == 0 {
			f.preview, f.program = f.program, n
		} else {
			m := f.mixes[mix+1]
			f.mixes[mix+1] = [2]int{n, m[0]}
		}
	}
	var acts []string
	before := [3]bool{f.recording, f.streaming, f.external}
	switch lname {
	case "previewinput":
		if mix == 0 {
			f.preview = in
		} else {
			m := f.mixes[mix+1]
			f.mixes[mix+1] = [2]int{m[0], in}
		}
	case "cut", "fade", "merge", "stinger1", "cutdirect", "activeinput":
		target := f.preview
		if mix != 0 {
			target = f.mixes[mix+1][1]
		}
		if q.Get("Input") != "" {
			target = in
		}
		setPgm(target)
	case "fadetoblack":
		f.ftb = !f.ftb
	case "startrecording":
		f.recording = true
	case "stoprecording":
		f.recording = false
	case "startstreaming":
		f.streaming = true
	case "stopstreaming":
		f.streaming = false
	case "startexternal":
		f.external = true
	case "stopexternal":
		f.external = false
	case "startmulticorder":
		f.multicorder = true
	case "stopmulticorder":
		f.multicorder = false
	case "setfader":
		f.fader, _ = strconv.Atoi(q.Get("Value"))
	case "setmastervolume":
		f.masterVol, _ = strconv.Atoi(q.Get("Value"))
	case "masteraudiooff":
		f.masterMuted = true
	case "masteraudioon":
		f.masterMuted = false
	case "busxaudiooff":
		f.busMuted[q.Get("Value")] = true
	case "setvolume":
		if in >= 1 && in <= len(f.inputs) {
			f.inputs[in-1].volume, _ = strconv.Atoi(q.Get("Value"))
		}
	case "audiooff", "audioon":
		if in >= 1 && in <= len(f.inputs) {
			f.inputs[in-1].muted = lname == "audiooff"
		}
	case "settext":
		if in >= 1 && in <= len(f.inputs) {
			for i, fl := range f.inputs[in-1].fields {
				if fl[0] == q.Get("SelectedName") {
					f.inputs[in-1].fields[i][1] = q.Get("Value")
				}
			}
		}
	case "selectindex":
		if in >= 1 && in <= len(f.inputs) {
			f.inputs[in-1].selected, _ = strconv.Atoi(q.Get("Value"))
		}
	case "play":
		if in >= 1 && in <= len(f.inputs) {
			f.inputs[in-1].state = "Running"
		}
	default:
		if n, ok := strings.CutPrefix(lname, "overlayinput"); ok {
			ch := int(n[0] - '0')
			switch n[1:] {
			case "in":
				f.overlays[ch] = in
			case "out", "off":
				f.overlays[ch] = 0
			case "":
				if f.overlays[ch] == in {
					f.overlays[ch] = 0
				} else {
					f.overlays[ch] = in
				}
			}
			acts = append(acts, fmt.Sprintf("Overlay%d %d 1", ch, in))
		}
	}
	if before[0] != f.recording {
		acts = append(acts, "Recording "+map[bool]string{true: "1", false: "0"}[f.recording])
	}
	if before[1] != f.streaming {
		acts = append(acts, "Streaming "+map[bool]string{true: "1", false: "0"}[f.streaming])
	}
	if before[2] != f.external {
		acts = append(acts, "External "+map[bool]string{true: "1", false: "0"}[f.external])
	}
	acts = append(acts, fmt.Sprintf("Input %d 1", f.program))
	f.pushLocked(acts)
	return true, ""
}

// pushLocked sends TALLY / ACTS to the subscribers (f.mu held).
func (f *fakeVmix) pushLocked(acts []string) {
	tally := f.tallyLocked()
	for c, s := range f.subs {
		var lines []string
		if s.tally {
			lines = append(lines, "TALLY OK "+tally)
		}
		if s.acts {
			for _, a := range acts {
				lines = append(lines, "ACTS OK "+a)
			}
		}
		if len(lines) > 0 {
			go func(c net.Conn, wmu *sync.Mutex, lines []string) {
				wmu.Lock()
				defer wmu.Unlock()
				for _, l := range lines {
					c.Write([]byte(l + "\r\n"))
				}
			}(c, s.wmu, lines)
		}
	}
}

// ---------------------------------------------------------------- HTTP

func (f *fakeVmix) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	f.mu.Lock()
	pw := f.password
	f.mu.Unlock()
	if pw != "" {
		u, p, ok := r.BasicAuth()
		if !ok || u != "admin" || p != pw {
			w.Header().Set("WWW-Authenticate", `Basic realm="vMix", charset="UTF-8"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Authentication required"))
			return
		}
	}
	if strings.TrimRight(strings.ToLower(r.URL.Path), "/") != "/api" {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	if fn := r.Form.Get("Function"); fn != "" {
		ok, msg := f.run("http", fn, r.Form)
		if !ok {
			w.WriteHeader(500)
			w.Write([]byte(msg))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("Function completed successfully."))
		return
	}
	f.mu.Lock()
	f.xmlReads++
	x := f.xmlLocked()
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Write([]byte(x))
}

// ----------------------------------------------------------------- TCP

func (f *fakeVmix) acceptTCP() {
	for {
		c, err := f.tcpLn.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		rej := f.reject
		if !rej {
			f.tcpConns[c] = true
		}
		f.mu.Unlock()
		if rej {
			c.Close()
			continue
		}
		go f.serveTCP(c)
	}
}

func (f *fakeVmix) serveTCP(c net.Conn) {
	wmu := &sync.Mutex{}
	sub := &fSub{wmu: wmu}
	defer func() {
		f.mu.Lock()
		delete(f.tcpConns, c)
		delete(f.subs, c)
		f.mu.Unlock()
		c.Close()
	}()
	write := func(s string) {
		wmu.Lock()
		c.Write([]byte(s))
		wmu.Unlock()
	}
	write("VERSION OK 29.0.0.50\r\n")
	br := bufio.NewReader(c)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd, arg, _ := strings.Cut(line, " ")
		switch cmd {
		case "VERSION":
			write("VERSION OK 29.0.0.50\r\n")
		case "PING":
			f.mu.Lock()
			np := f.noPong
			f.mu.Unlock()
			if !np {
				write("PING OK PONG\r\n")
			}
		case "XML":
			f.mu.Lock()
			f.xmlReads++
			x := f.xmlLocked() + "\r\n"
			f.mu.Unlock()
			write(fmt.Sprintf("XML %d\r\n%s", len(x), x))
		case "TALLY":
			f.mu.Lock()
			t := f.tallyLocked()
			f.mu.Unlock()
			write("TALLY OK " + t + "\r\n")
		case "SUBSCRIBE", "UNSUBSCRIBE":
			on := cmd == "SUBSCRIBE"
			switch arg {
			case "TALLY":
				f.mu.Lock()
				sub.tally = on
				f.subs[c] = sub
				t := f.tallyLocked()
				f.mu.Unlock()
				write(cmd + " OK TALLY Subscribed\r\n")
				if on {
					write("TALLY OK " + t + "\r\n")
				}
			case "ACTS":
				f.mu.Lock()
				sub.acts = on
				f.subs[c] = sub
				f.mu.Unlock()
				write(cmd + " OK ACTS Subscribed\r\n")
			default:
				write(cmd + " ER Not Supported\r\n")
			}
		case "FUNCTION":
			name, qs, _ := strings.Cut(arg, " ")
			q, _ := url.ParseQuery(qs)
			if ok, msg := f.run("tcp", name, q); !ok && msg == "Function not supported over API" {
				write("FUNCTION ER " + msg + "\r\n")
			} else {
				write("FUNCTION OK Completed\r\n") // vMix says OK even for unknown names
			}
		case "QUIT", "GET", "POST":
			write("QUIT OK Bye\r\n")
			return
		default:
			write(cmd + " ER Unknown Command\r\n")
		}
	}
}

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sample = `<vmix><version>29.0.0.49</version><edition>Max</edition>
<inputs>
 <input key="b" number="2" type="NDI" title="Proclaim Slides" state="Running">Proclaim</input>
 <input key="a" number="1" type="Capture" title="PTZ Wide" state="Running">PTZ</input>
</inputs><preview>2</preview><active>1</active>
<streaming>True</streaming><recording>False</recording><fadeToBlack>False</fadeToBlack></vmix>`

func TestDriverAgainstFakeVmix(t *testing.T) {
	var calls []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f := r.URL.Query().Get("Function"); f != "" {
			calls = append(calls, r.URL.RawQuery)
			return
		}
		w.Write([]byte(sample))
	}))
	defer ts.Close()
	c := New(ts.URL)
	ctx := context.Background()
	st, err := c.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Inputs) != 2 || st.Inputs[0].Title != "PTZ Wide" || st.Preview != 2 || !st.Streaming || st.Recording {
		t.Fatalf("%+v", st)
	}
	c.Preview(ctx, 3)
	c.Cut(ctx)
	c.Fade(ctx, 750)
	want := []string{"Function=PreviewInput&Input=3", "Function=Cut", "Duration=750&Function=Fade"}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("call %d: %q want %q", i, calls[i], want[i])
		}
	}
	if _, err := ParseXML([]byte("<html/>")); err == nil {
		t.Fatal("non-vmix XML accepted")
	}
}

func TestUnreachable(t *testing.T) {
	if _, err := New("http://127.0.0.1:1").State(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

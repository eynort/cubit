package mc

import (
	"context"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/server"
	"github.com/mj41/go-mc26/chat"
	mcnet "github.com/mj41/go-mc26/net"
)

// fakeStatus levanta un servidor que solo contesta al estado (en 127.0.0.1).
func fakeStatus(t *testing.T, name string, protocol int) string {
	t.Helper()
	l, err := mcnet.ListenMC("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	players := server.NewPlayerList(20)
	s := &server.Server{ListPingHandler: struct {
		*server.PingInfo
		*server.PlayerList
	}{server.NewPingInfo(name, protocol, chat.Text("fake"), nil), players}}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go s.AcceptConn(&c)
		}
	}()
	return l.Addr().String()
}

func TestPingFakeServer(t *testing.T) {
	addr := fakeStatus(t, "Paper 1.21.4", 769)
	st, err := Ping(context.Background(), addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if st.Protocol != 769 || st.Name != "Paper 1.21.4" || st.Version != "1.21.4" || st.Max != 20 {
		t.Fatalf("estado %+v", st)
	}
}

func TestPingNobody(t *testing.T) {
	if _, err := Ping(context.Background(), "127.0.0.1:1", 2*time.Second); err == nil {
		t.Fatal("sin servidor tendría que dar error")
	}
}

func TestParseStatus(t *testing.T) {
	cases := []struct {
		in       string
		proto    int
		ver      string
		wantFail bool
	}{
		{`{"version":{"name":"1.8.x","protocol":47},"players":{"max":10,"online":2}}`, 47, "1.8", false},
		{`{"version":{"name":"Velocity 1.7.2-1.21.4","protocol":769}}`, 769, "1.7.2", false},
		{`{"version":{"name":"Spigot","protocol":5}}`, 5, "", false},
		{`{"description":"x"}`, 0, "", true},
		{`no json`, 0, "", true},
	}
	for _, c := range cases {
		st, err := ParseStatus([]byte(c.in))
		if (err != nil) != c.wantFail {
			t.Fatalf("%s: err=%v", c.in, err)
		}
		if err == nil && (st.Protocol != c.proto || st.Version != c.ver) {
			t.Fatalf("%s: %+v", c.in, st)
		}
	}
}

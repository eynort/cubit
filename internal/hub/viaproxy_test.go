package hub

import (
	"net"
	"testing"

	"github.com/mj41/go-mc26-kit/bot"

	"cubit/internal/mc"
)

func TestViaState(t *testing.T) {
	defer func(v string) { ViaProxy = v }(ViaProxy)
	other := verInfo{st: mc.Status{Protocol: bot.ProtocolVersion + 1, Version: "1.21.4"}}
	same := verInfo{st: mc.Status{Protocol: bot.ProtocolVersion, Version: "26.2"}}

	ViaProxy = ""
	t.Setenv("MCBOT_VIAPROXY", "")
	if d, v := viaState(other); d != "1.21.4" || v != ViaUnavailable {
		t.Fatalf("sin ViaProxy: %q %q", d, v)
	}
	if _, v := viaState(same); v != ViaNone {
		t.Fatalf("misma versión: %q", v)
	}
	if d, v := viaState(verInfo{err: net.ErrClosed}); d != "" || v != ViaNone {
		t.Fatalf("con error: %q %q", d, v)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ViaProxy = l.Addr().String()
	if _, v := viaState(other); v != ViaOK {
		t.Fatalf("ViaProxy en marcha: %q", v)
	}
	l.Close()
	if _, v := viaState(other); v != ViaUnavailable {
		t.Fatalf("ViaProxy caído: %q", v)
	}
	ViaProxy = "/x/ViaProxy.jar" // (lo lanza el hub: disponible)
	if _, v := viaState(other); v != ViaOK {
		t.Fatalf("con jar: %q", v)
	}
}

package viaproxy

import (
	"context"
	"net"
	"reflect"
	"testing"

	"cubit/internal/mc"
)

func TestDecide(t *testing.T) {
	const ours = 800
	cases := []struct {
		name string
		st   mc.Status
		via  string
		want Mode
	}{
		{"misma versión", mc.Status{Protocol: 800, Version: "26.2"}, "", Direct},
		{"misma con via", mc.Status{Protocol: 800}, "x:1", Direct},
		{"otra sin via", mc.Status{Protocol: 769, Version: "1.21.4"}, "", Unavailable},
		{"otra con via", mc.Status{Protocol: 47, Version: "1.8"}, "127.0.0.1:25568", Via},
		{"sin protocolo", mc.Status{}, "x:1", Direct},
	}
	for _, c := range cases {
		if got := Decide(ours, c.st, c.via).Mode; got != c.want {
			t.Errorf("%s: %v, quiero %v", c.name, got, c.want)
		}
	}
}

func TestWildcardHost(t *testing.T) {
	if got, want := WildcardHost("mc.example.com", 25570, "1.21.4"), "address.mc.example.com.port.25570.version.1.21.4.f2.viaproxy.cubit"; got != want {
		t.Fatalf("%s != %s", got, want)
	}
	if got, want := WildcardHost("[::1]", 25565, ""), "address.::1.port.25565.f2.viaproxy.cubit"; got != want {
		t.Fatalf("%s != %s", got, want)
	}
	if got, want := ConnectAddr("a.b", 1, "1.8"), "address.a.b.port.1.version.1.8.f2.viaproxy.cubit:1"; got != want {
		t.Fatalf("%s != %s", got, want)
	}
}

func TestCommand(t *testing.T) {
	prog, args := Config{Jar: "/opt/ViaProxy.jar"}.Command()
	want := []string{"-Xmx192m", "-jar", "/opt/ViaProxy.jar", "cli", "--bind-address", "127.0.0.1:25568",
		"--wildcard-domain-handling", "public", "--auth-method", "none"}
	if prog != "java" || !reflect.DeepEqual(args, want) {
		t.Fatalf("%s %v", prog, args)
	}
	_, args = Config{Java: "/jdk/bin/java", Jar: "v.jar", Bind: "0.0.0.0:1", Auth: "account", Heap: "-Xmx64m"}.Command()
	if args[0] != "-Xmx64m" || args[5] != "0.0.0.0:1" || args[9] != "account" {
		t.Fatalf("%v", args)
	}
	if !IsJar("C:\\x\\ViaProxy.JAR") || IsJar("viaproxy:25568") {
		t.Fatal("IsJar")
	}
}

func TestDialerIgnoresAddr(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	got := make(chan struct{}, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			got <- struct{}{}
			c.Close()
		}
	}()
	c, err := Dialer{Via: l.Addr().String()}.DialMCContext(context.Background(), "address.nope.invalid.port.1.f2.viaproxy.cubit:1")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	<-got
}

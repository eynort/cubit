package main

// En Android (compilado sin cgo, p.ej. desde el PC con GOOS=android) Go busca
// los DNS en /etc/resolv.conf, que Android no tiene, y acaba preguntando a
// localhost: no resuelve play.example.net. Aquí se usan los de Termux
// ($PREFIX/etc/resolv.conf) y, si no hay, los públicos de Google.

import (
	"bufio"
	"context"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func init() {
	if _, err := os.Stat("/etc/resolv.conf"); err == nil {
		return
	}
	servers := []string{"8.8.8.8:53", "8.8.4.4:53"}
	if f, err := os.Open(os.Getenv("PREFIX") + "/etc/resolv.conf"); err == nil {
		var own []string
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if fs := strings.Fields(sc.Text()); len(fs) == 2 && fs[0] == "nameserver" {
				own = append(own, net.JoinHostPort(fs[1], "53"))
			}
		}
		f.Close()
		if len(own) > 0 {
			servers = own
		}
	}
	var next atomic.Int64
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			s := servers[int(next.Add(1)-1)%len(servers)]
			return d.DialContext(ctx, network, s)
		},
	}
}

package main

// Otras versiones de Minecraft: antes de entrar se pregunta la versión del
// servidor (Server List Ping); si no es la del bot, se entra por ViaProxy
// (internal/viaproxy). El hub lanza o conoce ViaProxy y le pasa su dirección
// al bot en MCBOT_VIAPROXY.

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/mj41/go-mc26-kit/bot"

	"cubit/internal/mc"
	"cubit/internal/mcserver"
	"cubit/internal/viaproxy"
)

var viaFlag = flag.String("viaproxy", "", "ViaProxy para servidores de otra versión de Minecraft: host:puerto de uno ya en marcha, o (solo con --hub) la ruta a ViaProxy-<versión>.jar para que el hub lo lance. También MCBOT_VIAPROXY")

// viaAddr: la dirección host:puerto de ViaProxy (vacía: no hay).
func viaAddr() string {
	if a := os.Getenv("MCBOT_VIAPROXY"); a != "" {
		return a
	}
	if *viaFlag != "" && !viaproxy.IsJar(*viaFlag) {
		return *viaFlag
	}
	return ""
}

// viaActive: la conexión actual va por ViaProxy (la cuenta de Microsoft la pone ViaProxy).
var viaActive bool

// route decide cómo entrar: la dirección, las opciones y si va por ViaProxy.
// Si no se puede preguntar la versión, entra directo (como siempre).
func route(ctx context.Context, addr string) (string, bot.JoinOptions, error) {
	viaActive = false
	opts := bot.JoinOptions{Context: ctx}
	if os.Getenv("MCBOT_ADDR") != "" || mcserver.IsTestServer(addr) {
		return addr, opts, nil
	}
	st, err := mc.Ping(ctx, addr, 6*time.Second)
	if err != nil {
		log.Printf("no pude preguntar la versión de %s (%v): entro directo", addr, err)
		return addr, opts, nil
	}
	d := viaproxy.Decide(bot.ProtocolVersion, st, viaAddr())
	switch d.Mode {
	case viaproxy.Unavailable:
		return addr, opts, fmt.Errorf("el servidor %s es de Minecraft %s (protocolo %d) y el bot habla %s: hace falta ViaProxy (--viaproxy host:puerto o MCBOT_VIAPROXY; ver docs/GUIA.md, «Otras versiones de Minecraft»)",
			addr, firstNonEmpty(st.Name, "?"), st.Protocol, versionName())
	case viaproxy.Via:
		host, p := mc.SplitAddr(addr)
		port, _ := strconv.Atoi(p)
		if err := waitVia(ctx, viaAddr()); err != nil {
			return addr, opts, err
		}
		viaActive = true
		log.Printf("el servidor es de Minecraft %s: entro por ViaProxy (%s)", firstNonEmpty(d.Version, st.Name), viaAddr())
		if *microsoftFlag {
			log.Print("ojo: por ViaProxy entra la cuenta que tenga ViaProxy, no la de Microsoft del bot")
		}
		opts.MCDialer = viaproxy.Dialer{Via: viaAddr()}
		return viaproxy.ConnectAddr(host, port, d.Version), opts, nil
	}
	return addr, opts, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// waitVia espera (hasta 25 s: el hub puede estar arrancándolo) a que ViaProxy acepte conexiones.
func waitVia(ctx context.Context, a string) error {
	deadline := time.Now().Add(25 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", a, 2*time.Second)
		if err == nil {
			_ = c.Close()
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("ViaProxy no responde en %s (¿está en marcha? ver docs/GUIA.md, «Otras versiones de Minecraft»): %w", a, err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

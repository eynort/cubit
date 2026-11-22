// Package viaproxy: entrar en servidores de otras versiones de Minecraft a
// través de ViaProxy (https://github.com/ViaVersion/ViaProxy).
//
// Un solo ViaProxy sirve para todos los servidores: se arranca con
// --wildcard-domain-handling public y el cliente le dice el destino en el
// nombre del host del handshake (formato 2 de ViaProxy):
//
//	address.<host>.port.<puerto>.version.<versión>.f2.viaproxy.cubit
//
// El bot se conecta por TCP a la dirección de ViaProxy, pero escribe ese nombre
// en el handshake (ver Dialer). Sin versión, ViaProxy la detecta solo.
//
// Cuentas: el bot entra en ViaProxy sin autenticar (modo offline); si el
// servidor de destino es premium, es ViaProxy quien entra con SU cuenta
// (auth-method account), la misma para todos los bots.
package viaproxy

import (
	"context"
	"fmt"
	"strings"

	mcnet "github.com/mj41/go-mc26/net"

	"cubit/internal/mc"
)

// Version: la versión de ViaProxy fijada (la del Dockerfile.viaproxy).
const Version = "3.4.14"

// DefaultBind: dónde escucha el ViaProxy que lanza el hub (solo en local).
const DefaultBind = "127.0.0.1:25568"

// Mode: cómo entra el bot en un servidor.
type Mode int

const (
	Direct      Mode = iota // misma versión (o no se pudo saber): directo
	Via                     // otra versión: por ViaProxy
	Unavailable             // otra versión y no hay ViaProxy
)

// Decision: el resultado de Decide.
type Decision struct {
	Mode    Mode
	Version string // versión del servidor ("" si no se pudo leer del nombre)
}

// Decide: si el servidor (st) habla otro protocolo que el nuestro (ours), hay
// que ir por ViaProxy (viaAddr no vacío) o no se puede (Unavailable).
func Decide(ours int, st mc.Status, viaAddr string) Decision {
	if st.Protocol == 0 || st.Protocol == ours {
		return Decision{Mode: Direct, Version: st.Version}
	}
	if strings.TrimSpace(viaAddr) == "" {
		return Decision{Mode: Unavailable, Version: st.Version}
	}
	return Decision{Mode: Via, Version: st.Version}
}

// Describe: texto para el panel ("1.21.4" o "1.21.4 · vía ViaProxy").
func Describe(d Decision) string {
	if d.Version == "" {
		return ""
	}
	return d.Version
}

// WildcardHost: el nombre de host del handshake que le dice a ViaProxy el
// destino (host, puerto) y la versión (vacía: que la detecte).
func WildcardHost(host string, port int, version string) string {
	s := fmt.Sprintf("address.%s.port.%d", strings.Trim(host, "[]"), port)
	if version != "" {
		s += ".version." + version
	}
	return s + ".f2.viaproxy.cubit"
}

// ConnectAddr: la "dirección" que se le da a JoinServer (el nombre comodín con
// el puerto del destino; el Dialer la cambia por la de ViaProxy al conectar).
func ConnectAddr(host string, port int, version string) string {
	return fmt.Sprintf("%s:%d", WildcardHost(host, port, version), port)
}

// Dialer conecta siempre a ViaProxy (Via), sea cual sea la dirección que le pida el cliente.
type Dialer struct {
	Via   string
	Inner mcnet.MCDialer // nil: el de la librería
}

func (d Dialer) DialMCContext(ctx context.Context, _ string) (*mcnet.Conn, error) {
	in := d.Inner
	if in == nil {
		in = &mcnet.DefaultDialer
	}
	return in.DialMCContext(ctx, d.Via)
}

// Config: cómo se lanza ViaProxy desde el hub.
type Config struct {
	Java string // "java" por defecto
	Jar  string // ruta a ViaProxy-<versión>.jar
	Bind string // DefaultBind por defecto
	Auth string // "none" (por defecto) o "account" (servidores premium: cuenta añadida en ViaProxy)
	Heap string // "-Xmx192m" por defecto (el bot cabe en un móvil)
}

// Command: el programa y los argumentos que lanzan ViaProxy en modo CLI con
// un solo proceso para todos los destinos. Es lo mismo que pone Dockerfile.viaproxy.
func (c Config) Command() (string, []string) {
	java, bind, auth, heap := c.Java, c.Bind, c.Auth, c.Heap
	if java == "" {
		java = "java"
	}
	if bind == "" {
		bind = DefaultBind
	}
	if auth == "" {
		auth = "none"
	}
	if heap == "" {
		heap = "-Xmx192m"
	}
	return java, []string{heap, "-jar", c.Jar, "cli",
		"--bind-address", bind,
		"--wildcard-domain-handling", "public",
		"--auth-method", auth,
	}
}

// IsJar: el valor de --viaproxy es un .jar (lo lanza el hub) y no host:puerto.
func IsJar(v string) bool { return strings.HasSuffix(strings.ToLower(v), ".jar") }

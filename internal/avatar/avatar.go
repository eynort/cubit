// Package avatar: la cabeza de un bot para el panel, la que corresponde a cómo se le ve:
//
//   - en un servidor premium (entra con su cuenta de Microsoft) lleva la skin
//     de esa cuenta: /avatar/<nombre>?skin=real, la cara de su skin de Mojang
//     (con la capa de encima);
//   - en uno sin cuentas premium el servidor no manda skins (cada cliente le
//     pone una por defecto): una cabeza pixelada propia sacada del nombre
//     (siempre la misma), para que no parezca que lleva la de esa cuenta.
//
// Las de Mojang se guardan en avatares/ (las que no existen, un día) para no
// preguntar cada vez; sin internet, también la pixelada.
package avatar

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cubit/internal/mc"
)

const (
	avatarDir  = "avatares"
	avatarSize = 64 // 8×8 de la cara, ampliada sin suavizar
	avatarMiss = 24 * time.Hour
)

var avatarMu sync.Mutex // (una descarga a la vez)

// Serve responde a /avatar/<name> con la cabeza de name (?skin=real: la de
// su cuenta de Mojang).
func Serve(w http.ResponseWriter, r *http.Request, name string) {
	name = strings.TrimSuffix(name, ".png")
	if !mc.ValidName.MatchString(name) {
		http.Error(w, "nombre", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "max-age=3600")
	if r.URL.Query().Get("skin") == "real" {
		_, _ = w.Write(avatarPNG(name))
		return
	}
	_, _ = w.Write(encodeFace(madeUpFace(name)))
}

// avatarPNG: la cabeza de name (de la caché, de Mojang o inventada).
func avatarPNG(name string) []byte {
	low := strings.ToLower(name)
	file := filepath.Join(avatarDir, low+".png")
	miss := filepath.Join(avatarDir, low+".no")
	if b, err := os.ReadFile(file); err == nil {
		return b
	}
	if st, err := os.Stat(miss); err != nil || time.Since(st.ModTime()) > avatarMiss {
		avatarMu.Lock()
		face, err := mojangFace(name)
		avatarMu.Unlock()
		_ = os.MkdirAll(avatarDir, 0o755)
		if err == nil {
			b := encodeFace(face)
			_ = os.WriteFile(file, b, 0o644)
			return b
		}
		_ = os.WriteFile(miss, []byte(err.Error()+"\n"), 0o644)
	}
	return encodeFace(madeUpFace(name))
}

// mojangFace: la cara (8×8, con la capa de encima) de la skin de esa cuenta.
func mojangFace(name string) (*image.RGBA, error) {
	c := http.Client{Timeout: 4 * time.Second}
	get := func(u string, v any) error {
		resp, err := c.Get(u)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s: %s", u, resp.Status)
		}
		return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
	}
	var prof struct{ ID string }
	if err := get("https://api.mojang.com/users/profiles/minecraft/"+name, &prof); err != nil || prof.ID == "" {
		return nil, fmt.Errorf("no es una cuenta de Minecraft: %v", err)
	}
	var sess struct {
		Properties []struct{ Name, Value string }
	}
	if err := get("https://sessionserver.mojang.com/session/minecraft/profile/"+prof.ID, &sess); err != nil {
		return nil, err
	}
	var skinURL string
	for _, p := range sess.Properties {
		if p.Name != "textures" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(p.Value)
		if err != nil {
			return nil, err
		}
		var t struct {
			Textures struct{ SKIN struct{ URL string } }
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		skinURL = t.Textures.SKIN.URL
	}
	if !strings.HasPrefix(skinURL, "http://textures.minecraft.net/") && !strings.HasPrefix(skinURL, "https://textures.minecraft.net/") {
		return nil, fmt.Errorf("sin skin")
	}
	resp, err := c.Get(skinURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	skin, err := png.Decode(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	face := image.NewRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(face, face.Bounds(), skin, image.Pt(8, 8), draw.Src) // la cara
	// la capa de encima (pelo, gafas…), si la tiene: en las skins antiguas esa
	// zona es opaca entera (negra o de un color) y taparía la cara
	solid := true
	for y := 8; y < 16 && solid; y++ {
		for x := 40; x < 48; x++ {
			if _, _, _, a := skin.At(x, y).RGBA(); a < 0xffff {
				solid = false
				break
			}
		}
	}
	if !solid {
		draw.Draw(face, face.Bounds(), skin, image.Pt(40, 8), draw.Over)
	}
	return face, nil
}

// madeUpFace: una cabeza de 8×8 propia, siempre la misma para un nombre:
// pelo, piel, ojos y boca con colores sacados del nombre.
func madeUpFace(name string) *image.RGBA {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	x := h.Sum64()
	pick := func(pal []color.RGBA) color.RGBA { c := pal[x%uint64(len(pal))]; x /= uint64(len(pal)); return c }
	skin := pick([]color.RGBA{{0xe8, 0xb8, 0x96, 255}, {0xc6, 0x8e, 0x6a, 255}, {0x8d, 0x5a, 0x3c, 255}, {0x5c, 0x3a, 0x26, 255}, {0xf1, 0xcf, 0xb0, 255}})
	hair := pick([]color.RGBA{{0x2b, 0x1d, 0x14, 255}, {0x6b, 0x4a, 0x2b, 255}, {0xc9, 0xa2, 0x4d, 255}, {0x1c, 0x1c, 0x1c, 255}, {0x9b, 0x3b, 0x26, 255}, {0x55, 0x7a, 0x9e, 255}, {0x5a, 0x8a, 0x4e, 255}})
	eye := pick([]color.RGBA{{0x3b, 0x5b, 0x9a, 255}, {0x4a, 0x7a, 0x3a, 255}, {0x5a, 0x3a, 0x22, 255}, {0x2a, 0x2a, 0x2a, 255}})
	style := x % 4
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	shade := func(c color.RGBA, k float64) color.RGBA {
		return color.RGBA{uint8(float64(c.R) * k), uint8(float64(c.G) * k), uint8(float64(c.B) * k), 255}
	}
	for yy := 0; yy < 8; yy++ {
		for xx := 0; xx < 8; xx++ {
			img.Set(xx, yy, skin)
		}
	}
	// pelo: arriba, y por los lados según el estilo
	for xx := 0; xx < 8; xx++ {
		img.Set(xx, 0, hair)
		img.Set(xx, 1, shade(hair, 0.9))
	}
	if style >= 1 {
		img.Set(0, 2, hair)
		img.Set(7, 2, hair)
	}
	if style >= 2 {
		img.Set(0, 3, shade(hair, 0.85))
		img.Set(7, 3, shade(hair, 0.85))
	}
	// ojos (blanco + iris), nariz y boca
	white := color.RGBA{0xf4, 0xf4, 0xf4, 255}
	img.Set(1, 4, white)
	img.Set(2, 4, eye)
	img.Set(5, 4, eye)
	img.Set(6, 4, white)
	img.Set(3, 5, shade(skin, 0.85))
	img.Set(4, 5, shade(skin, 0.85))
	mouth := shade(skin, 0.6)
	img.Set(3, 6, mouth)
	img.Set(4, 6, mouth)
	if style == 3 { // barba
		for xx := 1; xx < 7; xx++ {
			img.Set(xx, 7, shade(hair, 0.9))
		}
		img.Set(2, 6, shade(hair, 0.9))
		img.Set(5, 6, shade(hair, 0.9))
	}
	return img
}

// encodeFace: la cara de 8×8, ampliada a avatarSize sin suavizar, en PNG.
func encodeFace(face *image.RGBA) []byte {
	k := avatarSize / 8
	big := image.NewRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	for y := 0; y < avatarSize; y++ {
		for x := 0; x < avatarSize; x++ {
			c := face.RGBAAt(x/k, y/k)
			if c.A == 0 { // (la capa de encima sin la cara de debajo: opaco)
				c.A = 255
			}
			big.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, big)
	return buf.Bytes()
}

// URL: la cabeza que toca en el panel a un bot con ese nombre; premium:
// juega con cuenta de Microsoft (lleva su skin de verdad).
func URL(name string, premium bool) string {
	u := "/avatar/" + url.PathEscape(name)
	if premium {
		u += "?skin=real"
	}
	return u
}

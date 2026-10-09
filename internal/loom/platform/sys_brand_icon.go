package platform

// sys_brand_icon.go — l'icône de la marque Loom, rendue à la volée.
//
// RÉPLIQUE EXACTE du favicon et du logo de l'UI web : carré à coins arrondis NOIR
// + tissage Loom écru (deux fils de chaîne, deux de trame, dessus-dessous ; le
// logo maître est docs/brand/loom-mark.svg). Une seule source pour tous les usages —
// zone de notification Windows, barre de menus macOS, icône du .exe — pour
// garantir une cohérence visuelle parfaite.
//
// Aucun asset binaire à committer : tout est dessiné ici, et l'icône du .exe est
// produite par `go generate ./cmd/loom` (voir tools/gen-icon).

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

const TrayIconSize = 32

var (
	brandBlack = color.RGBA{0x0d, 0x0d, 0x0d, 0xff}
	brandWhite = color.RGBA{0xf4, 0xf1, 0xea, 0xff}
	brandClear = color.RGBA{0, 0, 0, 0}
)

// weavePiece : un morceau de fil du tissage Loom sur la grille 256 du logo
// (docs/brand/loom-mark.svg). Deux fils de chaîne et deux de trame passent
// dessus-dessous ; un bout arrondi est une extrémité libre, un bout droit
// s'arrête au ras d'un fil qui passe par-dessus.
type weavePiece struct {
	x0, y0, x1, y1                   float64
	horizontal, roundStart, roundEnd bool
}

// loomWeave : même géométrie que le SVG maître (épaisseur 40, rayon 20, jour 12).
var loomWeave = []weavePiece{
	{20, 64, 52, 104, true, true, false}, {116, 64, 236, 104, true, false, true},
	{20, 152, 140, 192, true, true, false}, {204, 152, 236, 192, true, false, true},
	{64, 20, 104, 140, false, true, false}, {64, 204, 104, 236, false, false, true},
	{152, 20, 192, 52, false, true, false}, {152, 116, 192, 236, false, false, true},
}

// inside teste un point de la grille 256 contre un morceau : rectangle, plus un
// demi-disque à chaque bout arrondi.
func (w weavePiece) inside(x, y float64) bool {
	const r = 20.0
	if w.horizontal {
		cy := (w.y0 + w.y1) / 2
		if y < w.y0 || y > w.y1 {
			return false
		}
		a, b := w.x0, w.x1
		if w.roundStart {
			a += r
			if x < a {
				return math.Hypot(x-a, y-cy) <= r
			}
		}
		if w.roundEnd {
			b -= r
			if x > b {
				return math.Hypot(x-b, y-cy) <= r
			}
		}
		return x >= a && x <= b
	}
	cx := (w.x0 + w.x1) / 2
	if x < w.x0 || x > w.x1 {
		return false
	}
	a, b := w.y0, w.y1
	if w.roundStart {
		a += r
		if y < a {
			return math.Hypot(x-cx, y-a) <= r
		}
	}
	if w.roundEnd {
		b -= r
		if y > b {
			return math.Hypot(x-cx, y-b) <= r
		}
	}
	return y >= a && y <= b
}

// brandIconImage dessine l'icône à la taille n. bg peint le carré arrondi, fg le
// motif de tissage. Les deux peuvent être transparents : c'est ce qui produit l'icône
// « template » de macOS.
func brandIconImage(n int, bg, fg color.RGBA) *image.RGBA {
	const r = 5.2 // rayon des coins sur grille 24
	outside := func(gx, gy float64) bool {
		if gx < 0.8 || gx > 23.2 || gy < 0.8 || gy > 23.2 {
			return true
		}
		corner := func(cx, cy float64) bool {
			dx, dy := gx-cx, gy-cy
			return dx*dx+dy*dy > r*r
		}
		// Les arcs partent du bord de la tuile (marge m) pour lui être tangents.
		const m = 0.8
		lo, hi := m+r, 24-m-r
		switch {
		case gx < lo && gy < lo:
			return corner(lo, lo)
		case gx > hi && gy < lo:
			return corner(hi, lo)
		case gx < lo && gy > hi:
			return corner(lo, hi)
		case gx > hi && gy > hi:
			return corner(hi, hi)
		}
		return false
	}

	img := image.NewRGBA(image.Rect(0, 0, n, n))
	scale := float64(n) / 24.0

	// Supersampling 4x4 pour un antialiasing net et doux à toute échelle
	const sub = 6
	const subW = 1.0 / float64(sub*sub)

	for py := 0; py < n; py++ {
		for px := 0; px < n; px++ {
			bgCov := 0.0
			fgCov := 0.0

			for sy := 0; sy < sub; sy++ {
				for sx := 0; sx < sub; sx++ {
					gx := (float64(px) + (float64(sx)+0.5)/sub) / scale
					gy := (float64(py) + (float64(sy)+0.5)/sub) / scale
					if !outside(gx, gy) {
						bgCov += subW
						// Le logo (grille 256) occupe le centre de la tuile, de 4 à 20.
						mx, my := (gx-4)*16, (gy-4)*16
						for _, w := range loomWeave {
							if w.inside(mx, my) {
								fgCov += subW
								break
							}
						}
					}
				}
			}

			if bgCov <= 0.001 {
				img.Set(px, py, brandClear)
				continue
			}

			// Composition fg sur bg avec couverture sous-pixel
			var c color.RGBA
			if fg == brandClear {
				// Mode template : le tissage est découpé (alpha 0)
				alpha := uint8(math.Round(float64(bg.A) * bgCov * (1.0 - fgCov)))
				c = color.RGBA{R: bg.R, G: bg.G, B: bg.B, A: alpha}
			} else {
				alpha := uint8(math.Round(float64(bg.A) * bgCov))
				rCol := float64(bg.R)*(1.0-fgCov) + float64(fg.R)*fgCov
				gCol := float64(bg.G)*(1.0-fgCov) + float64(fg.G)*fgCov
				bCol := float64(bg.B)*(1.0-fgCov) + float64(fg.B)*fgCov
				c = color.RGBA{
					R: uint8(math.Round(rCol)),
					G: uint8(math.Round(gCol)),
					B: uint8(math.Round(bCol)),
					A: alpha,
				}
			}
			img.Set(px, py, c)
		}
	}
	return img
}

func encodePNG(img *image.RGBA) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// BrandIconPNG rend l'icône de marque Loom en PNG de n pixels.
func BrandIconPNG(n int) []byte { return encodePNG(brandIconImage(n, brandBlack, brandWhite)) }

// brandTemplatePNG rend la variante « template » macOS (tissage découpé).
//
//lint:ignore U1000 utilisée par sys_tray_darwin.go, invisible sans CGO/macOS
func BrandTemplatePNG(n int) []byte {
	return encodePNG(brandIconImage(n, brandBlack, brandClear))
}

// BrandICO emballe une ou plusieurs tailles PNG dans un conteneur .ico. Windows
// accepte le PNG comme image d'une entrée .ico (alpha conservé pour les coins
// arrondis). Plusieurs tailles = un rendu net partout, de la barre des tâches
// (16 px) à la grande tuile de l'explorateur (256 px).
func BrandICO(sizes ...int) []byte {
	if len(sizes) == 0 {
		sizes = []int{TrayIconSize}
	}
	imgs := make([][]byte, 0, len(sizes))
	for _, n := range sizes {
		imgs = append(imgs, BrandIconPNG(n))
	}
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, uint16(0))         // réservé
	binary.Write(&ico, binary.LittleEndian, uint16(1))         // type = icône
	binary.Write(&ico, binary.LittleEndian, uint16(len(imgs))) // nombre d'images
	offset := uint32(6 + 16*len(imgs))                         // fin du répertoire
	for i, p := range imgs {
		n := sizes[i]
		// 256 px se code 0 dans un .ico (le champ ne fait qu'un octet).
		ico.WriteByte(byte(n % 256))
		ico.WriteByte(byte(n % 256))
		ico.WriteByte(0)                                        // couleurs de la palette (0 = truecolor)
		ico.WriteByte(0)                                        // réservé
		binary.Write(&ico, binary.LittleEndian, uint16(1))      // plans
		binary.Write(&ico, binary.LittleEndian, uint16(32))     // bits/pixel
		binary.Write(&ico, binary.LittleEndian, uint32(len(p))) // taille données
		binary.Write(&ico, binary.LittleEndian, offset)         // offset données
		offset += uint32(len(p))
	}
	for _, p := range imgs {
		ico.Write(p)
	}
	return ico.Bytes()
}

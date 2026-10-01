package platform

// sys_brand_icon.go — l'icône de la marque Loom, rendue à la volée.
//
// RÉPLIQUE EXACTE du favicon et du logo de l'UI web : carré à coins arrondis NOIR
// + tissage Loom écru (3 fils horizontaux, 3 fils verticaux entrecroisés avec
// extrémités arrondies sur grille 24x24). Une seule source pour tous les usages —
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

type lineSeg struct {
	x1, y1, x2, y2 float64
}

// loomSegments : les 6 fils du tissage Loom sur une grille 24x24 (3 horizontaux, 3 verticaux).
var loomSegments = []lineSeg{
	{5.0, 8.5, 19.0, 8.5},
	{5.0, 12.0, 19.0, 12.0},
	{5.0, 15.5, 19.0, 15.5},
	{8.5, 5.0, 8.5, 19.0},
	{12.0, 5.0, 12.0, 19.0},
	{15.5, 5.0, 15.5, 19.0},
}

func segDist(px, py, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(px-x1, py-y1)
	}
	t := ((px-x1)*dx + (py-y1)*dy) / l2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return math.Hypot(px-(x1+t*dx), py-(y1+t*dy))
}

// brandIconImage dessine l'icône à la taille n. bg peint le carré arrondi, fg le
// motif de tissage. Les deux peuvent être transparents : c'est ce qui produit l'icône
// « template » de macOS.
func brandIconImage(n int, bg, fg color.RGBA) *image.RGBA {
	const r = 5.2 // rayon des coins sur grille 24
	const strokeR = 0.82
	outside := func(gx, gy float64) bool {
		if gx < 0.8 || gx > 23.2 || gy < 0.8 || gy > 23.2 {
			return true
		}
		corner := func(cx, cy float64) bool {
			dx, dy := gx-cx, gy-cy
			return dx*dx+dy*dy > r*r
		}
		switch {
		case gx < r && gy < r:
			return corner(r, r)
		case gx > 24-r && gy < r:
			return corner(24-r, r)
		case gx < r && gy > 24-r:
			return corner(r, 24-r)
		case gx > 24-r && gy > 24-r:
			return corner(24-r, 24-r)
		}
		return false
	}

	img := image.NewRGBA(image.Rect(0, 0, n, n))
	scale := float64(n) / 24.0

	// Supersampling 4x4 pour un antialiasing net et doux à toute échelle
	const sub = 4
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
						minD := 1e9
						for _, s := range loomSegments {
							d := segDist(gx, gy, s.x1, s.y1, s.x2, s.y2)
							if d < minD {
								minD = d
							}
						}
						if minD <= strokeR {
							fgCov += subW
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

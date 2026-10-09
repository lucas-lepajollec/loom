# Loom brand mark

Two warp and two weft threads woven over and under: Loom weaves models, coding
agents, machines and the Brain into one station. Monochrome, no gradient.

| File | Use |
| --- | --- |
| `loom-mark.svg` | Master mark, ink `#141413`, for light backgrounds. |
| `loom-mark-light.svg` | Same mark, `#f4f1ea`, for dark backgrounds. |
| `loom-favicon.svg` | Browser tab icon; follows the system light/dark scheme. |
| `loom-app-icon.svg` | App tile (installed web app, demo): mark at 2/3 on a `#0d0d0d` rounded tile. |

The standalone mark never sits on a decorative tile. The tile exists only where
a platform needs an opaque icon: the executable, macOS bundle, tray and
installed web-app icons, rasterized by `internal/loom/platform/sys_brand_icon.go`
from the same geometry (`go run ./tools/gen-icon icon.ico` in `cmd/loom`).

Geometry on a 256 grid: threads 40 wide with 20-radius free ends, warp at
x 64–104 and 152–192, weft at y 64–104 and 152–192, extent 20–236, and a
12-unit gap where a thread passes under another.

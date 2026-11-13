package service

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"

	"github.com/addp/common/inference"
)

// segmentGeoReference maps source pixel corners to the GeoTIFF's CRS.
type segmentGeoReference struct {
	affine [6]float64
	epsg   uint16
}

// readSegmentGeoReference reads the classic TIFF primary IFD. Segment input
// already uses the classic TIFF image decoder, and the first two supported
// CRS can be converted to WGS84 without a separate projection runtime.
func readSegmentGeoReference(data []byte) (segmentGeoReference, error) {
	var ref segmentGeoReference
	unsupported := fmt.Errorf("%w: GeoTIFF requires a PixelIsArea affine transform in EPSG:4326 or EPSG:3857", ErrSegmentImageUnsupported)
	if len(data) < 8 {
		return ref, unsupported
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return ref, unsupported
	}
	if order.Uint16(data[2:]) != 42 {
		return ref, unsupported
	}
	offset := int(order.Uint32(data[4:]))
	if offset < 8 || offset > len(data)-2 {
		return ref, unsupported
	}
	n := int(order.Uint16(data[offset:]))
	if n > (len(data)-offset-2)/12 {
		return ref, unsupported
	}
	var keys []uint16
	var scale, tie, matrix []float64
	seen := map[uint16]bool{}
	for i := range n {
		entry := data[offset+2+i*12:]
		tag, kind := order.Uint16(entry), order.Uint16(entry[2:])
		count, position := int(order.Uint32(entry[4:])), int(order.Uint32(entry[8:]))
		switch tag {
		case 34735:
			if kind != 3 || count < 4 || count > (len(data)-position)/2 || seen[tag] {
				return ref, unsupported
			}
			keys = make([]uint16, count)
			for j := range keys {
				keys[j] = order.Uint16(data[position+2*j:])
			}
		case 33550, 33922, 34264:
			want := map[uint16]int{33550: 3, 33922: 6, 34264: 16}[tag]
			if kind != 12 || count != want || count > (len(data)-position)/8 || seen[tag] {
				return ref, unsupported
			}
			values := make([]float64, count)
			for j := range values {
				v := math.Float64frombits(order.Uint64(data[position+8*j:]))
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return ref, unsupported
				}
				values[j] = v
			}
			switch tag {
			case 33550:
				scale = values
			case 33922:
				tie = values
			case 34264:
				matrix = values
			}
		default:
			continue
		}
		seen[tag] = true
	}
	if len(keys) < 4 || keys[0] != 1 || 4+4*int(keys[3]) > len(keys) {
		return ref, unsupported
	}
	var model, raster, geographic, projected uint16
	for i := range int(keys[3]) {
		key := keys[4+4*i:]
		switch key[0] {
		case 1024, 1025, 2048, 3072:
			if key[1] != 0 || key[2] != 1 {
				return ref, unsupported
			}
			switch key[0] {
			case 1024:
				model = key[3]
			case 1025:
				raster = key[3]
			case 2048:
				geographic = key[3]
			case 3072:
				projected = key[3]
			}
		}
	}
	switch {
	case raster != 1:
		return ref, unsupported
	case model == 1 && projected == 3857:
		ref.epsg = projected
	case model == 2 && geographic == 4326:
		ref.epsg = geographic
	default:
		return ref, unsupported
	}
	switch {
	case len(matrix) != 0 && len(scale) == 0 && len(tie) == 0:
		if matrix[12] != 0 || matrix[13] != 0 || matrix[14] != 0 || matrix[15] != 1 {
			return ref, unsupported
		}
		ref.affine = [6]float64{matrix[3], matrix[0], matrix[1], matrix[7], matrix[4], matrix[5]}
	case len(matrix) == 0 && len(scale) == 3 && len(tie) == 6 && scale[0] > 0 && scale[1] > 0:
		ref.affine = [6]float64{tie[3] - scale[0]*tie[0], scale[0], 0, tie[4] + scale[1]*tie[1], 0, -scale[1]}
	default:
		return ref, unsupported
	}
	a := ref.affine
	det := a[1]*a[5] - a[2]*a[4]
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return ref, unsupported
	}
	return ref, nil
}

type segmentVectorFeature struct {
	Type       string                  `json:"type"`
	Geometry   segmentVectorGeometry   `json:"geometry"`
	Properties segmentVectorProperties `json:"properties"`
}

type segmentVectorGeometry struct {
	Type        string           `json:"type"`
	Coordinates [][][][2]float64 `json:"coordinates"`
}

type segmentVectorProperties struct {
	InstanceID int     `json:"instance_id"`
	Score      float64 `json:"score"`
}

type segmentPixelRing struct {
	points []image.Point
	area2  int
}

type segmentPixelEdge struct {
	at  image.Point
	dir int
}

var segmentPixelDirections = [...]image.Point{{X: 1}, {Y: 1}, {X: -1}, {Y: -1}}

// segmentMaskRings traces the boundary of foreground pixel cells. A right turn
// at a diagonal contact keeps four-connected components separate.
func segmentMaskRings(mask image.Image) []segmentPixelRing {
	bounds := mask.Bounds()
	foreground := func(p image.Point) bool {
		if !p.In(bounds) {
			return false
		}
		r, _, _, a := mask.At(p.X, p.Y).RGBA()
		return a != 0 && r >= 0x8000
	}
	edges := make(map[segmentPixelEdge]bool)
	order := make([]segmentPixelEdge, 0)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			p := image.Pt(x, y)
			if !foreground(p) {
				continue
			}
			corners := [...]image.Point{p, p.Add(image.Pt(1, 0)), p.Add(image.Pt(1, 1)), p.Add(image.Pt(0, 1))}
			for dir := range corners {
				if !foreground(p.Add(segmentPixelDirections[(dir+3)%4])) {
					e := segmentPixelEdge{corners[dir], dir}
					edges[e] = true
					order = append(order, e)
				}
			}
		}
	}
	var rings []segmentPixelRing
	for _, start := range order {
		if !edges[start] {
			continue
		}
		ring := segmentPixelRing{points: []image.Point{start.at}}
		current := start
		for {
			delete(edges, current)
			next := current.at.Add(segmentPixelDirections[current.dir])
			ring.area2 += current.at.X*next.Y - next.X*current.at.Y
			closed := false
			for _, turn := range [...]int{1, 0, 3} {
				candidate := segmentPixelEdge{next, (current.dir + turn) % 4}
				if candidate == start {
					closed = true
					break
				}
				if edges[candidate] {
					if turn != 0 {
						ring.points = append(ring.points, next)
					}
					current = candidate
					break
				}
			}
			if closed {
				break
			}
		}
		ring.points = append(ring.points, start.at)
		rings = append(rings, ring)
	}
	return rings
}

func segmentRingContains(ring segmentPixelRing, x, y float64) bool {
	inside := false
	for i := 1; i < len(ring.points); i++ {
		a, b := ring.points[i-1], ring.points[i]
		// Traced edges are axis aligned, so only vertical edges cross the ray.
		if a.X == b.X && float64(a.X) > x && (float64(a.Y) > y) != (float64(b.Y) > y) {
			inside = !inside
		}
	}
	return inside
}

func segmentVectors(masks []inference.InstanceMask, ref segmentGeoReference, window ImageWindow, width, height int) ([]byte, int, error) {
	if width <= 0 || height <= 0 || width > 1024 || height > 1024 || window.Width <= 0 || window.Height <= 0 {
		return nil, 0, ErrSegmentImageUnsupported
	}
	features := make([]segmentVectorFeature, 0, len(masks))
	a := ref.affine
	for i, mask := range masks {
		if mask.Width <= 0 || mask.Height <= 0 || mask.X < 0 || mask.Y < 0 ||
			mask.X > width-mask.Width || mask.Y > height-mask.Height || math.IsNaN(mask.Score) || math.IsInf(mask.Score, 0) {
			return nil, 0, ErrSegmentImageUnsupported
		}
		data, err := base64.StdEncoding.DecodeString(mask.PNG)
		if err != nil {
			return nil, 0, ErrSegmentImageUnsupported
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width != mask.Width || cfg.Height != mask.Height {
			return nil, 0, ErrSegmentImageUnsupported
		}
		bitmap, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, 0, ErrSegmentImageUnsupported
		}
		rings := segmentMaskRings(bitmap)
		var shells, holes []segmentPixelRing
		for _, ring := range rings {
			if ring.area2 > 0 {
				shells = append(shells, ring)
			} else {
				holes = append(holes, ring)
			}
		}
		if len(shells) == 0 {
			continue
		}
		project := func(r segmentPixelRing) ([][2]float64, error) {
			coords := make([][2]float64, len(r.points))
			for j, p := range r.points {
				x := float64(window.X) + float64(mask.X+p.X)*float64(window.Width)/float64(width)
				y := float64(window.Y) + float64(mask.Y+p.Y)*float64(window.Height)/float64(height)
				lon, lat := a[0]+a[1]*x+a[2]*y, a[3]+a[4]*x+a[5]*y
				if ref.epsg == 3857 {
					lon *= 180 / (math.Pi * 6378137)
					lat = math.Atan(math.Sinh(lat/6378137)) * 180 / math.Pi
				}
				if math.IsNaN(lon) || math.IsNaN(lat) || math.Abs(lon) > 180 || math.Abs(lat) > 90 {
					return nil, ErrSegmentImageUnsupported
				}
				if a[1]*a[5]-a[2]*a[4] < 0 {
					coords[len(coords)-1-j] = [2]float64{lon, lat}
				} else {
					coords[j] = [2]float64{lon, lat}
				}
			}
			return coords, nil
		}
		polygons := make([][][][2]float64, len(shells))
		for j, shell := range shells {
			coords, err := project(shell)
			if err != nil {
				return nil, 0, err
			}
			polygons[j] = [][][2]float64{coords}
		}
		for _, hole := range holes {
			p, q := hole.points[0], hole.points[1]
			dx, dy := float64(q.X-p.X), float64(q.Y-p.Y)
			offset := 0.25 / (math.Abs(dx) + math.Abs(dy))
			// Sample just to the left of the first directed edge, inside the hole.
			x := (float64(p.X)+float64(q.X))/2 + offset*dy
			y := (float64(p.Y)+float64(q.Y))/2 - offset*dx
			shell := -1
			for j, candidate := range shells {
				if segmentRingContains(candidate, x, y) && (shell < 0 || candidate.area2 < shells[shell].area2) {
					shell = j
				}
			}
			if shell < 0 {
				return nil, 0, ErrSegmentImageUnsupported
			}
			coords, err := project(hole)
			if err != nil {
				return nil, 0, err
			}
			polygons[shell] = append(polygons[shell], coords)
		}
		features = append(features, segmentVectorFeature{"Feature", segmentVectorGeometry{"MultiPolygon", polygons}, segmentVectorProperties{i + 1, mask.Score}})
	}
	data, err := json.Marshal(struct {
		Type     string                 `json:"type"`
		Features []segmentVectorFeature `json:"features"`
	}{"FeatureCollection", features})
	return data, len(features), err
}

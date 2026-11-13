package image

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/spatial"
)

var _ format.MultiMediaInfoProvider = (*tiffPlugin)(nil)

func (p *tiffPlugin) DescribeMultiMedia(ctx context.Context, reader contentio.Reader, refs []format.RelatedRef, options *format.ParseOptions) (*format.MediaDescribeResult, error) {
	primary, err := format.PrimaryRelatedRef(refs)
	if err != nil {
		return nil, err
	}
	stream, err := reader.Open(ctx, primary.Ref)
	if err != nil {
		return nil, err
	}
	info, err := p.DescribeMedia(ctx, stream, options)
	closeErr := stream.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if info.Media == nil {
		return info, nil
	}

	// Priority is per fact: PAM > INTERNAL > WORLDFILE. Only registered refs
	// are eligible, and the canonical PAM name follows GDAL's TIFF contract.
	var pam *format.RelatedRef
	var world *format.RelatedRef
	worldRank := 4
	for i := range refs {
		ref := &refs[i]
		if ref.Ref.Role == "auxiliary_metadata" && strings.EqualFold(ref.Ref.Path, primary.Ref.Path+".aux.xml") {
			pam = ref
		}
		if ref.Ref.Role == "world_file" {
			rank := 4
			switch strings.ToLower(path.Ext(ref.Ref.Path)) {
			case ".tfw":
				rank = 0
			case ".tifw":
				rank = 1
			case ".wld":
				rank = 2
			}
			if rank < worldRank {
				world = ref
				worldRank = rank
			}
		}
	}
	var crs string
	var transform *[6]float64
	if pam != nil {
		raw, err := readTIFFSidecar(ctx, reader, pam.Ref, 1<<20)
		if err != nil {
			return nil, err
		}
		crs, transform, err = parseTIFFPAM(raw)
		if err != nil {
			return nil, err
		}
	}
	if transform == nil && (info.Spatial == nil || info.Spatial.Extent == nil) && world != nil {
		raw, err := readTIFFSidecar(ctx, reader, world.Ref, 4096)
		if err != nil {
			return nil, err
		}
		values, err := parseTIFFAffine(strings.Fields(string(raw)))
		if err != nil {
			return nil, err
		}
		// A,D,B,E,C,F in the world file refer to the first pixel's center.
		gt := [6]float64{values[4] - (values[0]+values[2])/2, values[0], values[2], values[5] - (values[1]+values[3])/2, values[1], values[3]}
		transform = &gt
	}
	if crs == "" && transform == nil {
		return info, nil
	}
	if info.Spatial == nil {
		noIndex := false
		info.Spatial = &datatype.SpatialInfo{HasSpatialIndex: &noIndex}
	}
	if crs != "" {
		info.Spatial.SRID = nil
		info.Spatial.CRSRef = ""
		info.Spatial.CRSDefinitions = nil
		if srid := spatial.ParseSRID(crs); srid > 0 {
			info.Spatial.SRID = &srid
		} else {
			info.Spatial.CRSRef = datatype.CustomCRSRef(crs)
			info.Spatial.CRSDefinitions = []datatype.CRSDefinition{{ID: info.Spatial.CRSRef, DefinitionEncoding: datatype.CRSDefinitionEncodingWKT, Definition: crs, Source: datatype.CRSDefinitionSourceSidecarPAM}}
		}
	}
	if transform != nil {
		determinant := transform[1]*transform[5] - transform[2]*transform[4]
		if determinant == 0 || math.IsNaN(determinant) || math.IsInf(determinant, 0) {
			return nil, fmt.Errorf("TIFF auxiliary transform has degenerate axes")
		}
		bbox := datatype.BoundingBox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for _, pixel := range [][2]float64{{0, 0}, {float64(info.Media.Width), 0}, {0, float64(info.Media.Height)}, {float64(info.Media.Width), float64(info.Media.Height)}} {
			x := transform[0] + pixel[0]*transform[1] + pixel[1]*transform[2]
			y := transform[3] + pixel[0]*transform[4] + pixel[1]*transform[5]
			if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
				return nil, fmt.Errorf("TIFF auxiliary transform produces non-finite extent")
			}
			bbox[0] = math.Min(bbox[0], x)
			bbox[1] = math.Min(bbox[1], y)
			bbox[2] = math.Max(bbox[2], x)
			bbox[3] = math.Max(bbox[3], y)
		}
		info.Spatial.Extent = &bbox
	}
	return info, nil
}

func readTIFFSidecar(ctx context.Context, reader contentio.Reader, ref contentio.Ref, limit int64) ([]byte, error) {
	stream, err := reader.Open(ctx, ref)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(stream, limit+1))
	closeErr := stream.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("TIFF %s exceeds metadata read budget", ref.Role)
	}
	return raw, nil
}

func parseTIFFPAM(raw []byte) (string, *[6]float64, error) {
	var pam struct {
		XMLName   xml.Name `xml:"PAMDataset"`
		SRS       []string `xml:"SRS"`
		Transform []string `xml:"GeoTransform"`
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&pam); err != nil {
		return "", nil, fmt.Errorf("invalid TIFF PAM: %w", err)
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
		if text, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(text)) != "" {
			return "", nil, fmt.Errorf("invalid trailing TIFF PAM content")
		}
	}
	if len(pam.SRS) > 1 || len(pam.Transform) > 1 {
		return "", nil, fmt.Errorf("duplicate TIFF PAM spatial declaration")
	}
	crs := ""
	if len(pam.SRS) == 1 {
		crs = strings.TrimSpace(pam.SRS[0])
	}
	if len(pam.Transform) == 0 {
		return crs, nil, nil
	}
	gt, err := parseTIFFAffine(strings.Split(pam.Transform[0], ","))
	if err != nil {
		return "", nil, err
	}
	return crs, &gt, nil
}

func parseTIFFAffine(fields []string) ([6]float64, error) {
	var values [6]float64
	if len(fields) != 6 {
		return values, fmt.Errorf("TIFF auxiliary transform requires six numbers")
	}
	for i, field := range fields {
		value, err := strconv.ParseFloat(strings.TrimSpace(field), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return values, fmt.Errorf("invalid TIFF auxiliary transform number")
		}
		values[i] = value
	}
	return values, nil
}

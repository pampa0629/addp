package osgb

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	MetadataFileName     = "metadata.xml"
	maxMetadataXMLBytes  = 1024 * 1024
	defaultOSGBDataDir   = "Data"
	osgbModelKind        = datatype.Model3DKindPhotogrammetryScene
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register OSGB format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatOSGB
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-osgb",
		Format:   format.FormatOSGB,
		I18nKey:  "format.osgb",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutWhole},
		Identification: format.FormatIdentification{
			Extensions: []string{".osgb"},
			FileNames:  []string{MetadataFileName},
			MimeTypes: []string{
				"application/octet-stream",
			},
		},
	}
}

func (p *Plugin) DescribeModel3DScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	if reader == nil {
		return nil, fmt.Errorf("OSGB scope reader is required")
	}
	manifestPath := path.Join(strings.Trim(scope.Path, "/"), MetadataFileName)
	if strings.Trim(manifestPath, "/") == "" {
		manifestPath = MetadataFileName
	}
	rc, err := reader.Open(ctx, contentio.NewRef(manifestPath, contentio.RoleMain))
	if err != nil {
		return nil, fmt.Errorf("open OSGB metadata: %w", err)
	}
	defer rc.Close()
	return p.DescribeModel3D(ctx, rc, options)
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	doc, err := DecodeMetadata(input, maxMetadataXMLBytes)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return describeMetadata(doc), nil
}

type MetadataDocument struct {
	XMLName   xml.Name        `xml:"ModelMetadata"`
	Version   string          `xml:"version,attr"`
	SRS       string          `xml:"SRS"`
	SRSOrigin string          `xml:"SRSOrigin"`
	Texture   MetadataTexture `xml:"Texture"`
}

type MetadataTexture struct {
	ColorSource string `xml:"ColorSource"`
}

func DecodeMetadata(input io.Reader, maxBytes int64) (*MetadataDocument, error) {
	if maxBytes <= 0 {
		maxBytes = maxMetadataXMLBytes
	}
	limited := io.LimitReader(input, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read OSGB metadata: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("OSGB metadata too large: %d", len(data))
	}
	var doc MetadataDocument
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse OSGB metadata: %w", err)
	}
	if doc.XMLName.Local != "ModelMetadata" {
		return nil, fmt.Errorf("invalid OSGB metadata: root element %q", doc.XMLName.Local)
	}
	if strings.TrimSpace(doc.SRS) == "" {
		return nil, fmt.Errorf("invalid OSGB metadata: missing SRS")
	}
	return &doc, nil
}

func describeMetadata(doc *MetadataDocument) *format.Model3DDescribeResult {
	if doc == nil {
		return nil
	}
	modelInfo := datatype.NormalizeModel3DInfo(&datatype.Model3DInfo{
		ModelKind: osgbModelKind,
	})
	formatInfo := map[string]interface{}{
		"manifest_ref": MetadataFileName,
		"data_dir":     defaultOSGBDataDir,
	}
	if value := strings.TrimSpace(doc.Version); value != "" {
		formatInfo["metadata_version"] = value
	}
	if value := strings.TrimSpace(doc.SRS); value != "" {
		formatInfo["srs"] = value
	}
	if origin := parseOrigin(doc.SRSOrigin); len(origin) == 3 {
		formatInfo["srs_origin"] = origin
	}
	if value := strings.TrimSpace(doc.Texture.ColorSource); value != "" {
		formatInfo["color_source"] = value
	}
	return &format.Model3DDescribeResult{
		Model3D:    modelInfo,
		Spatial:    spatialInfoFromSRS(doc.SRS),
		FormatInfo: formatInfo,
	}
}

func spatialInfoFromSRS(srs string) *datatype.SpatialInfo {
	normalized := strings.TrimSpace(strings.ToUpper(srs))
	if !strings.HasPrefix(normalized, "EPSG:") {
		return nil
	}
	code, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(normalized, "EPSG:")))
	if err != nil || code <= 0 {
		return nil
	}
	return &datatype.SpatialInfo{
		SRID:   &code,
		CRSRef: datatype.EPSGCRSRef(code),
	}
}

func parseOrigin(value string) []float64 {
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) != 3 {
		return nil
	}
	origin := make([]float64, 0, 3)
	for _, part := range parts {
		number, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil
		}
		origin = append(origin, number)
	}
	return origin
}

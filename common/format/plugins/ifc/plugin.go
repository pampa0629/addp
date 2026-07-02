package ifc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const maxIFCScanLines = 200_000

var (
	ifcEntityLinePattern = regexp.MustCompile(`(?i)^\s*#\d+\s*=\s*([A-Z0-9_]+)\s*\(`)
	ifcSchemaPattern     = regexp.MustCompile(`(?i)FILE_SCHEMA\s*\(\s*\(([^)]*)\)\s*\)`)
	ifcStringPattern     = regexp.MustCompile(`'([^']*)'`)
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register IFC format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatIFC
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-ifc",
		Format:   format.FormatIFC,
		I18nKey:  "format.ifc",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".ifc"},
			MimeTypes:  []string{"application/x-step", "application/step", "model/ifc"},
			ContentSignatures: []string{
				"text:ISO-10303-21",
				"text:FILE_SCHEMA",
			},
		},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	upper := strings.ToUpper(strings.TrimSpace(string(peek)))
	return strings.HasPrefix(upper, "ISO-10303-21") && strings.Contains(upper, "FILE_SCHEMA")
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	summary, err := scanIFC(ctx, input)
	if err != nil {
		return nil, err
	}
	model := &datatype.Model3DInfo{
		ModelKind: datatype.Model3DKindBIMModel,
	}
	formatInfo := map[string]interface{}{
		"encoding":           "step",
		"schema_identifiers": summary.schemaIdentifiers,
		"entity_count":       summary.entityCount,
		"entity_type_counts": summary.entityTypeCounts,
		"scan_complete":      summary.scanComplete,
		"scanned_line_count": summary.scannedLineCount,
	}
	if summary.schemaVersion != "" {
		formatInfo["schema_version"] = summary.schemaVersion
	}
	return &format.Model3DDescribeResult{
		Model3D:    model,
		FormatInfo: formatInfo,
	}, nil
}

type ifcSummary struct {
	schemaIdentifiers []string
	schemaVersion     string
	entityCount       int64
	entityTypeCounts  map[string]int64
	scannedLineCount  int64
	scanComplete      bool
}

func scanIFC(ctx context.Context, input io.Reader) (ifcSummary, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	summary := ifcSummary{
		entityTypeCounts: map[string]int64{},
		scanComplete:     true,
	}
	for scanner.Scan() {
		summary.scannedLineCount++
		if summary.scannedLineCount > maxIFCScanLines {
			summary.scanComplete = false
			break
		}
		if summary.scannedLineCount%4096 == 0 {
			select {
			case <-ctx.Done():
				return summary, ctx.Err()
			default:
			}
		}
		line := scanner.Text()
		if len(summary.schemaIdentifiers) == 0 {
			summary.schemaIdentifiers = parseSchemaIdentifiers(line)
			if len(summary.schemaIdentifiers) > 0 {
				summary.schemaVersion = primarySchemaVersion(summary.schemaIdentifiers[0])
			}
		}
		matches := ifcEntityLinePattern.FindStringSubmatch(line)
		if len(matches) == 2 {
			entityType := strings.ToUpper(strings.TrimSpace(matches[1]))
			summary.entityCount++
			summary.entityTypeCounts[entityType]++
		}
	}
	if err := scanner.Err(); err != nil {
		return summary, err
	}
	return summary, nil
}

func parseSchemaIdentifiers(line string) []string {
	matches := ifcSchemaPattern.FindStringSubmatch(line)
	if len(matches) != 2 {
		return nil
	}
	rawSchemas := ifcStringPattern.FindAllStringSubmatch(matches[1], -1)
	schemas := make([]string, 0, len(rawSchemas))
	seen := map[string]struct{}{}
	for _, raw := range rawSchemas {
		if len(raw) != 2 {
			continue
		}
		schema := strings.ToUpper(strings.TrimSpace(raw[1]))
		if schema == "" {
			continue
		}
		if _, exists := seen[schema]; exists {
			continue
		}
		seen[schema] = struct{}{}
		schemas = append(schemas, schema)
	}
	return schemas
}

func primarySchemaVersion(schema string) string {
	schema = strings.ToUpper(strings.TrimSpace(schema))
	switch {
	case strings.HasPrefix(schema, "IFC4X3"):
		return "IFC4X3"
	case strings.HasPrefix(schema, "IFC4"):
		return "IFC4"
	case strings.HasPrefix(schema, "IFC2X3"):
		return "IFC2X3"
	default:
		return schema
	}
}

package extractors

import (
	"github.com/addp/common/format"
)

func init() {
	// 注册所有内置提取器到全局 format 注册表
	format.RegisterExtractor(&CSVExtractor{})
	format.RegisterExtractor(&GeoJSONExtractor{})
	format.RegisterExtractor(&ImageExtractor{})
	format.RegisterExtractor(&PDFExtractor{})
	format.RegisterExtractor(&ShapefileExtractor{})
	format.RegisterExtractor(&SQLiteExtractor{})
	format.RegisterExtractor(&DefaultExtractor{})
	format.RegisterExtractor(&VideoExtractor{})
	format.RegisterExtractor(&OfficeExtractor{})
}

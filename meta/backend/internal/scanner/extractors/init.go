package extractors

import (
	"github.com/addp/meta/internal/scanner"
)

// init 函数在包加载时自动执行，注册所有内置的元数据提取器
func init() {
	// 注册所有提取器
	// 注意：注册顺序不重要，因为注册表会根据优先级自动排序

	// 1. 注册地理空间数据提取器
	scanner.Register(&GeoJSONExtractor{})

	// 2. 注册表格数据提取器
	scanner.Register(&CSVExtractor{})

	// 3. 注册图像提取器
	scanner.Register(&ImageExtractor{})

	// 4. 注册文档提取器
	scanner.Register(&PDFExtractor{})

	// 5. 注册数据库提取器
	scanner.Register(&SQLiteExtractor{})

	// 6. 注册默认提取器（兜底，必须最后注册）
	scanner.Register(&DefaultExtractor{})

	// 未来可以在此添加更多提取器：
	// scanner.Register(&ParquetExtractor{})
	// scanner.Register(&ShapefileExtractor{})
	// scanner.Register(&ExcelExtractor{})
	// scanner.Register(&WordExtractor{})
	// scanner.Register(&VideoExtractor{})
	// scanner.Register(&AudioExtractor{})
}

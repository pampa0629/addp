package builtin

import (
	commonClient "github.com/addp/common/client"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
)

// init 自动注册所有内置预览插件
func init() {
	// 1. PostgreSQL 表预览
	service.RegisterPreviewProvider("postgresql-table", func(repo *repository.MetadataRepository, metaClient *commonClient.MetaClient, metaServiceURL string, _ *service.ObjectContentRegistry) (service.PreviewProvider, error) {
		return service.NewPostgresPreviewProvider(repo), nil
	})

	// 2. MySQL/Doris 表预览
	service.RegisterPreviewProvider("mysql-table", func(repo *repository.MetadataRepository, metaClient *commonClient.MetaClient, metaServiceURL string, _ *service.ObjectContentRegistry) (service.PreviewProvider, error) {
		return service.NewMySQLPreviewProvider(repo), nil
	})

	// 3. Shapefile 预览
	service.RegisterPreviewProvider("shapefile", func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, _ string, _ *service.ObjectContentRegistry) (service.PreviewProvider, error) {
		return service.NewShapefilePreviewProvider(), nil
	})

	// 3. CSV 预览
	service.RegisterPreviewProvider("csv", func(_ *repository.MetadataRepository, _ *commonClient.MetaClient, _ string, _ *service.ObjectContentRegistry) (service.PreviewProvider, error) {
		return service.NewCSVPreviewProvider(), nil
	})

	// 4. 对象存储预览
	service.RegisterPreviewProvider("object-storage", func(repo *repository.MetadataRepository, metaClient *commonClient.MetaClient, metaServiceURL string, content *service.ObjectContentRegistry) (service.PreviewProvider, error) {
		return service.NewObjectStoragePreviewProvider(repo, metaClient, metaServiceURL, content), nil
	})

	// 5. Schema 节点预览
	service.RegisterPreviewProvider("schema-node", func(repo *repository.MetadataRepository, metaClient *commonClient.MetaClient, metaServiceURL string, _ *service.ObjectContentRegistry) (service.PreviewProvider, error) {
		return service.NewSchemaPreviewProvider(repo, metaClient), nil
	})
}

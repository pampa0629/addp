package connector

import (
	"github.com/addp/transfer/pkg/pipeline"
)

// RegisterAllConnectors 注册所有内置连接器
func RegisterAllConnectors(registry *pipeline.ConnectorRegistry) error {
	// 注册 JDBC Reader
	if err := registry.RegisterReader("jdbc", NewJDBCReader); err != nil {
		return err
	}

	if err := registry.RegisterReader("mysql", NewJDBCReader); err != nil {
		return err
	}

	if err := registry.RegisterReader("postgresql", NewJDBCReader); err != nil {
		return err
	}

	if err := registry.RegisterReader("postgres", NewJDBCReader); err != nil {
		return err
	}

	// 注册 JDBC Writer
	if err := registry.RegisterWriter("jdbc", NewJDBCWriter); err != nil {
		return err
	}

	if err := registry.RegisterWriter("mysql", NewJDBCWriter); err != nil {
		return err
	}

	if err := registry.RegisterWriter("postgresql", NewJDBCWriter); err != nil {
		return err
	}

	if err := registry.RegisterWriter("postgres", NewJDBCWriter); err != nil {
		return err
	}

	// 注册 File Reader
	if err := registry.RegisterReader("file", func(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
		reader := NewFileReader()
		return reader, nil
	}); err != nil {
		return err
	}

	if err := registry.RegisterReader("csv", func(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
		reader := NewFileReader()
		return reader, nil
	}); err != nil {
		return err
	}

	if err := registry.RegisterReader("json", func(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
		reader := NewFileReader()
		return reader, nil
	}); err != nil {
		return err
	}

	if err := registry.RegisterReader("jsonl", func(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
		reader := NewFileReader()
		return reader, nil
	}); err != nil {
		return err
	}

	// 注册 File Writer
	if err := registry.RegisterWriter("file", func(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
		writer := NewFileWriter()
		return writer, nil
	}); err != nil {
		return err
	}

	if err := registry.RegisterWriter("csv", func(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
		writer := NewFileWriter()
		return writer, nil
	}); err != nil {
		return err
	}

	if err := registry.RegisterWriter("json", func(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
		writer := NewFileWriter()
		return writer, nil
	}); err != nil {
		return err
	}

	if err := registry.RegisterWriter("jsonl", func(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
		writer := NewFileWriter()
		return writer, nil
	}); err != nil {
		return err
	}

	// 注册 S3 Reader
	if err := registry.RegisterReader("s3", func(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
		reader := NewS3Reader()
		return reader, nil
	}); err != nil {
		return err
	}

	if err := registry.RegisterReader("minio", func(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
		reader := NewS3Reader()
		return reader, nil
	}); err != nil {
		return err
	}

	// 注册 S3 Writer
	if err := registry.RegisterWriter("s3", func(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
		writer := NewS3Writer()
		return writer, nil
	}); err != nil {
		return err
	}

	if err := registry.RegisterWriter("minio", func(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
		writer := NewS3Writer()
		return writer, nil
	}); err != nil {
		return err
	}

	return nil
}

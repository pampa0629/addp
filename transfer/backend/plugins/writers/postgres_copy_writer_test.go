package writers

import (
	"context"
	"testing"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/stretchr/testify/assert"
)

func TestPostgresCOPYWriter_WriteMode_DefaultValue(t *testing.T) {
	config := pipeline.ConnectorConfig{
		Type: "postgres_copy",
		Config: map[string]interface{}{
			"host":     "localhost",
			"port":     5432,
			"database": "test",
			"username": "test",
			"password": "test",
			"table":    "test_table",
		},
	}

	writer, err := NewPostgresCOPYWriter(config)
	assert.NoError(t, err)

	pgWriter := writer.(*PostgresCOPYWriter)
	assert.Equal(t, "insert", pgWriter.config.WriteMode, "默认写入模式应为 insert")
}

func TestPostgresCOPYWriter_WriteMode_Replace(t *testing.T) {
	config := pipeline.ConnectorConfig{
		Type: "postgres_copy",
		Config: map[string]interface{}{
			"host":       "localhost",
			"port":       5432,
			"database":   "test",
			"username":   "test",
			"password":   "test",
			"table":      "test_table",
			"write_mode": "replace",
		},
	}

	writer, err := NewPostgresCOPYWriter(config)
	assert.NoError(t, err)

	pgWriter := writer.(*PostgresCOPYWriter)
	assert.Equal(t, "replace", pgWriter.config.WriteMode, "应正确设置 replace 模式")
}

func TestPostgresCOPYWriter_WriteMode_UpsertNotSupported(t *testing.T) {
	config := pipeline.ConnectorConfig{
		Type: "postgres_copy",
		Config: map[string]interface{}{
			"host":       "localhost",
			"port":       5432,
			"database":   "test",
			"username":   "test",
			"password":   "test",
			"table":      "test_table",
			"write_mode": "upsert",
		},
	}

	_, err := NewPostgresCOPYWriter(config)
	assert.Error(t, err, "upsert 模式应返回错误")
	assert.Contains(t, err.Error(), "does not support upsert mode", "错误信息应包含不支持 upsert")
}

func TestPostgresCOPYWriter_WriteMode_InvalidMode(t *testing.T) {
	config := pipeline.ConnectorConfig{
		Type: "postgres_copy",
		Config: map[string]interface{}{
			"host":       "localhost",
			"port":       5432,
			"database":   "test",
			"username":   "test",
			"password":   "test",
			"table":      "test_table",
			"write_mode": "invalid_mode",
		},
	}

	_, err := NewPostgresCOPYWriter(config)
	assert.Error(t, err, "无效模式应返回错误")
	assert.Contains(t, err.Error(), "invalid write_mode", "错误信息应包含无效模式提示")
}

func TestPostgresCOPYWriter_EnsureTable_Replace(t *testing.T) {
	// 这个测试需要真实的数据库连接，所以先跳过
	// 在集成测试中验证 TRUNCATE 逻辑
	t.Skip("需要真实数据库连接，在集成测试中验证")

	config := pipeline.ConnectorConfig{
		Type: "postgres_copy",
		Config: map[string]interface{}{
			"host":         "localhost",
			"port":         5432,
			"database":     "test",
			"username":     "test",
			"password":     "test",
			"table":        "test_table",
			"write_mode":   "replace",
			"create_table": true,
		},
	}

	writer, err := NewPostgresCOPYWriter(config)
	assert.NoError(t, err)

	ctx := context.Background()
	err = writer.Open(ctx, config)
	assert.NoError(t, err)
	defer writer.Close()

	// 创建测试批次
	batch := &pipeline.DataBatch{
		Rows: []map[string]interface{}{
			{"id": 1, "name": "test"},
		},
		Schema: &pipeline.Schema{
			Fields: []pipeline.Field{
				{Name: "id", Type: "int", Nullable: false},
				{Name: "name", Type: "string", Nullable: true},
			},
		},
	}

	// 第一次写入
	err = writer.Write(ctx, batch)
	assert.NoError(t, err)
	err = writer.Flush(ctx)
	assert.NoError(t, err)

	// TODO: 验证表中有 1 条记录

	// 第二次写入（应该 TRUNCATE 后再写入）
	err = writer.Write(ctx, batch)
	assert.NoError(t, err)
	err = writer.Flush(ctx)
	assert.NoError(t, err)

	// TODO: 验证表中仍然只有 1 条记录（不是 2 条）
}

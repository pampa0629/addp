package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/addp/labs/vector/internal/client"
	"github.com/addp/labs/vector/internal/config"
	"github.com/addp/labs/vector/internal/service"
)

func main() {
	// 加载配置
	cfg := config.Load()

	// 初始化日志
	logger := initLogger(cfg.LogLevel)
	logger.Info("========== Vector Tool 启动 ==========")

	// 解析命令
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	ctx := context.Background()

	// 根据配置选择向量化客户端
	var embeddingClient client.EmbeddingClient

	switch cfg.EmbeddingServiceType {
	case "clip":
		logger.Info("使用 CLIP 向量化服务", "url", cfg.CLIPServiceURL)
		embeddingClient = client.NewCLIPClient(client.CLIPConfig{
			BaseURL: cfg.CLIPServiceURL,
		}, logger)
	case "dashscope":
		logger.Info("使用 DashScope 向量化服务")
		embeddingClient = client.NewDashScopeClient(client.DashScopeConfig{
			BaseURL: cfg.DashScopeBaseURL,
			APIKey:  cfg.DashScopeAPIKey,
		}, logger)
	default:
		logger.Error("未知的向量化服务类型", "type", cfg.EmbeddingServiceType)
		log.Fatalf("❌ 不支持的向量化服务: %s（支持: clip, dashscope）", cfg.EmbeddingServiceType)
	}

	// 初始化 PgVector 客户端
	pgvector, err := client.NewPgVectorClient(ctx, client.PgVectorConfig{
		DSN:    cfg.GetDSN(),
		Schema: cfg.DBSchema,
		Table:  cfg.DBTable,
	}, logger)

	if err != nil {
		log.Fatalf("❌ 数据库连接失败: %v", err)
	}
	defer pgvector.Close()

	// 创建服务
	svc := service.NewVectorService(embeddingClient, pgvector, logger)

	// 执行命令
	switch command {
	case "batch":
		handleBatch(ctx, svc, logger)
	case "search":
		handleSearch(ctx, svc, logger)
	case "search-text":
		handleSearchText(ctx, svc, logger)
	case "status":
		handleStatus(ctx, svc, logger)
	case "init":
		handleInit(ctx, embeddingClient, pgvector, logger)
	default:
		logger.Error("❌ 未知命令", "command", command)
		printUsage()
		os.Exit(1)
	}

	logger.Info("========== 执行完成 ==========")
}

// handleBatch 批量向量化
func handleBatch(ctx context.Context, svc *service.VectorService, logger *slog.Logger) {
	dataDir := "../data" // 默认路径
	if len(os.Args) > 2 {
		dataDir = os.Args[2]
	}

	absPath, _ := filepath.Abs(dataDir)
	logger.Info("批量向量化", "directory", absPath)

	if err := svc.BatchVectorize(ctx, absPath); err != nil {
		logger.Error("❌ 批量向量化失败", "error", err)
		os.Exit(1)
	}
}

// handleSearch 语义检索
func handleSearch(ctx context.Context, svc *service.VectorService, logger *slog.Logger) {
	if len(os.Args) < 3 {
		logger.Error("❌ 缺少查询图片路径")
		fmt.Println("用法: vector search <image_path> [top_k]")
		os.Exit(1)
	}

	queryPath := os.Args[2]
	topK := 5
	if len(os.Args) > 3 {
		if k, err := strconv.Atoi(os.Args[3]); err == nil {
			topK = k
		}
	}

	results, err := svc.SearchSimilarImages(ctx, queryPath, topK)
	if err != nil {
		logger.Error("❌ 语义检索失败", "error", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		logger.Info("未找到相似图片")
	}
}

// handleSearchText 文本语义检索
func handleSearchText(ctx context.Context, svc *service.VectorService, logger *slog.Logger) {
	if len(os.Args) < 3 {
		logger.Error("❌ 缺少查询文本")
		fmt.Println("用法: vector search-text <查询文本> [top_k]")
		fmt.Println("示例: vector search-text \"开会\" 5")
		os.Exit(1)
	}

	queryText := os.Args[2]
	topK := 5
	if len(os.Args) > 3 {
		if k, err := strconv.Atoi(os.Args[3]); err == nil {
			topK = k
		}
	}

	results, err := svc.SearchByText(ctx, queryText, topK)
	if err != nil {
		logger.Error("❌ 文本语义检索失败", "error", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		logger.Info("未找到相似图片")
	}
}

// handleStatus 查看数据库状态
func handleStatus(ctx context.Context, svc *service.VectorService, logger *slog.Logger) {
	if err := svc.GetStatus(ctx); err != nil {
		logger.Error("❌ 查询状态失败", "error", err)
		os.Exit(1)
	}
}

// handleInit 初始化（检测向量维度）
func handleInit(
	ctx context.Context,
	embedding client.EmbeddingClient,
	pgvector *client.PgVectorClient,
	logger *slog.Logger,
) {
	logger.Info("========== 开始初始化 ==========")
	logger.Info("此命令将调用一次 API 以检测向量维度")

	// 检查是否提供了测试图片
	testImagePath := "../data/test.jpg"
	if len(os.Args) > 2 {
		testImagePath = os.Args[2]
	}

	// 检查文件是否存在
	if _, err := os.Stat(testImagePath); os.IsNotExist(err) {
		logger.Error("❌ 测试图片不存在",
			"path", testImagePath,
			"提示", "请提供一张测试图片，或使用: vector init <image_path>")
		os.Exit(1)
	}

	logger.Info("使用测试图片", "path", testImagePath)

	// 提取向量
	vector, err := embedding.ExtractVectorFromImage(ctx, testImagePath)
	if err != nil {
		logger.Error("❌ 向量提取失败", "error", err)
		os.Exit(1)
	}

	dimension := len(vector)
	logger.Info("✅ 检测到向量维度", "dimension", dimension)

	// 设置数据库维度
	if err := pgvector.SetDimension(ctx, dimension); err != nil {
		logger.Error("❌ 设置维度失败", "error", err)
		os.Exit(1)
	}

	logger.Info("========== 初始化完成 ==========")
	logger.Info("提示", "message", "现在可以使用 'vector batch' 命令进行批量向量化")
}

// initLogger 初始化日志
func initLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: false,
	}

	handler := slog.NewTextHandler(os.Stdout, opts)
	return slog.New(handler)
}

// printUsage 打印使用说明
func printUsage() {
	fmt.Println(`
Vector Tool - 图片向量化和语义检索工具

用法:
  vector batch [directory]          批量向量化目录下所有图片（默认：../data）
  vector search <image> [top_k]     用图片查找相似图片（默认 top_k=5）
  vector search-text <文本> [top_k] 用文本查找相似图片（默认 top_k=5）
  vector status                     查看数据库状态
  vector init [test_image]          初始化数据库（检测向量维度）

示例:
  vector batch ../data                    # 向量化 ../data 目录
  vector search ../data/开会.jpg 10       # 用图片查找 Top 10 相似图片
  vector search-text "开会" 5             # 用文本查找 Top 5 相似图片
  vector search-text "meeting" 10         # 支持中英文文本检索
  vector status                           # 查看已存储的向量数量
  vector init ../data/test.jpg            # 使用测试图片初始化

注意:
  1. 首次使用前需要先运行 'vector init' 命令
  2. 确保 PostgreSQL 容器已启动: docker-compose up -d
  3. 确保 .env 文件中的 API Key 配置正确
`)
}

package main

import (
	"context"
	"lineage-demo/workflow"
	"lineage/models"
	"lineage/repository"
	"lineage/service"
	"lineage/storage"
	"log"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	// 1. 初始化 lineage 库（SQLite）
	db, err := storage.NewSQLiteDB("./lineage.db")
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	log.Println("✅ Database connected")

	// 2. 创建 Repository 和 Service（依赖注入）
	lineageRepo := repository.NewLineageRepository(db)
	lineageService := service.NewLineageService(lineageRepo)
	log.Println("✅ Lineage service initialized")

	// 3. 连接 Temporal Server
	c, err := client.Dial(client.Options{
		HostPort: "localhost:7233",
	})
	if err != nil {
		log.Fatalf("Failed to connect to Temporal server: %v", err)
	}
	defer c.Close()
	log.Println("✅ Connected to Temporal Server")

	// 4. 创建 Worker
	w := worker.New(c, "lineage-demo-queue", worker.Options{})

	// 5. 注册 Workflow
	w.RegisterWorkflow(workflow.DataTransformWorkflow)
	log.Println("✅ Workflow registered: DataTransformWorkflow")

	// 6. 注册 Activity（使用带依赖注入的 wrapper）
	activityWrapper := func(ctx context.Context, input interface{}) error {
		// 注入 lineageService 到 context
		ctx = context.WithValue(ctx, "lineage_recorder", lineageService)
		return workflow.RecordLineageActivity(ctx, input.(*models.LineageInput))
	}
	w.RegisterActivity(activityWrapper)
	log.Println("✅ Activity registered: RecordLineageActivity (with dependency injection)")

	// 7. 启动 Worker
	log.Println("🚀 Worker started, listening on queue: lineage-demo-queue")
	log.Println("   Press Ctrl+C to stop")
	err = w.Run(worker.InterruptCh())
	if err != nil {
		log.Fatalf("Worker failed: %v", err)
	}
}

package main

import (
	"context"
	"fmt"
	"lineage/models"
	"lineage/repository"
	"lineage/service"
	"lineage/storage"
	"log"
	"time"
)

func main() {
	// 1. 初始化数据库
	db, err := storage.NewSQLiteDB("./worker/lineage.db")
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	log.Println("✅ Database connected")

	// 2. 创建 Service
	lineageRepo := repository.NewLineageRepository(db)
	lineageService := service.NewLineageService(lineageRepo)
	log.Println("✅ Lineage service initialized")

	// 3. 直接记录一条血缘
	input := &models.LineageInput{
		ExternalWorkflowID:  "test-workflow-direct",
		ExternalExecutionID: "test-run-123",
		WorkflowEngine:      "manual",

		SourceItemID:      100,
		SourceFingerprint: "test-source-fp",
		SourceType:        "table",
		SourcePath:        "db.schema.test_source",

		TargetItemID:      200,
		TargetFingerprint: "test-target-fp",
		TargetType:        "table",
		TargetPath:        "db.schema.test_target",

		LineageType: "transform",
		Status:      "success",
		StartTime:   time.Now().Add(-10 * time.Second),
		EndTime:     time.Now(),

		RecordsProcessed: 500,
		BytesWritten:     25000,
		TransformConfig: map[string]interface{}{
			"operation": "test",
		},
	}

	lineage, err := lineageService.Record(context.Background(), input)
	if err != nil {
		log.Fatalf("❌ Failed to record lineage: %v", err)
	}

	log.Printf("✅ Lineage recorded successfully!")
	log.Printf("   ID: %d", lineage.ID)
	log.Printf("   Source: %s (ID: %d)", lineage.SourcePath, lineage.SourceItemID)
	log.Printf("   Target: %s (ID: %d)", lineage.TargetPath, lineage.TargetItemID)
	log.Printf("   Workflow ID: %s", lineage.ExternalWorkflowID)

	// 4. 查询验证
	all, err := lineageService.Query(context.Background(), &models.LineageQuery{})
	if err != nil {
		log.Fatalf("❌ Failed to query lineage: %v", err)
	}

	fmt.Printf("\n📊 Total lineage records: %d\n", len(all))
	for _, l := range all {
		fmt.Printf("  - [%d] %s -> %s (Workflow: %s)\n",
			l.ID, l.SourcePath, l.TargetPath, l.ExternalWorkflowID)
	}
}

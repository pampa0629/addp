package main

import (
	"context"
	"lineage-demo/workflow"
	"log"

	"go.temporal.io/sdk/client"
)

func main() {
	// 1. 连接 Temporal Server
	c, err := client.Dial(client.Options{
		HostPort: "localhost:7233",
	})
	if err != nil {
		log.Fatalf("Failed to connect to Temporal server: %v", err)
	}
	defer c.Close()
	log.Println("✅ Connected to Temporal Server")

	// 2. 启动 Workflow
	workflowOptions := client.StartWorkflowOptions{
		ID:        "lineage-demo-workflow-2",  // 使用新的 workflow ID
		TaskQueue: "lineage-demo-queue",
	}

	// 模拟数据转换：source_table_1 -> target_table_2
	sourceItemID := uint(1)
	targetItemID := uint(2)

	we, err := c.ExecuteWorkflow(context.Background(), workflowOptions, workflow.DataTransformWorkflow, sourceItemID, targetItemID)
	if err != nil {
		log.Fatalf("Failed to start workflow: %v", err)
	}

	log.Printf("🚀 Workflow started successfully")
	log.Printf("   Workflow ID: %s", we.GetID())
	log.Printf("   Run ID: %s", we.GetRunID())
	log.Printf("   Source Item: %d", sourceItemID)
	log.Printf("   Target Item: %d", targetItemID)

	// 3. 等待 Workflow 完成
	log.Println("⏳ Waiting for workflow to complete...")
	err = we.Get(context.Background(), nil)
	if err != nil {
		log.Fatalf("Workflow failed: %v", err)
	}

	log.Println("✅ Workflow completed successfully!")
	log.Println("   Check lineage.db for recorded lineage data")
	log.Println("   Run API server to query lineage: cd api && go run server.go")
}

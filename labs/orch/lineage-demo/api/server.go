package main

import (
	"lineage/models"
	"lineage/repository"
	"lineage/service"
	"lineage/storage"
	"log"
	"strconv"

	"github.com/gin-gonic/gin"
)

func main() {
	// 1. 初始化数据库
	db, err := storage.NewSQLiteDB("./lineage.db")
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	log.Println("✅ Database connected")

	// 2. 创建服务
	lineageRepo := repository.NewLineageRepository(db)
	lineageService := service.NewLineageService(lineageRepo)
	log.Println("✅ Lineage service initialized")

	// 3. 创建 Gin Router
	r := gin.Default()

	// CORS
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 4. 定义路由
	api := r.Group("/api/lineage")
	{
		// 查询所有血缘
		api.GET("/all", func(c *gin.Context) {
			lineages, err := lineageService.Query(c.Request.Context(), &models.LineageQuery{})
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, lineages)
		})

		// 查询上游血缘
		api.GET("/upstream/:item_id", func(c *gin.Context) {
			itemID, err := strconv.ParseUint(c.Param("item_id"), 10, 32)
			if err != nil {
				c.JSON(400, gin.H{"error": "invalid item_id"})
				return
			}
			lineages, err := lineageService.QueryUpstream(c.Request.Context(), uint(itemID))
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, lineages)
		})

		// 查询下游血缘
		api.GET("/downstream/:item_id", func(c *gin.Context) {
			itemID, err := strconv.ParseUint(c.Param("item_id"), 10, 32)
			if err != nil {
				c.JSON(400, gin.H{"error": "invalid item_id"})
				return
			}
			lineages, err := lineageService.QueryDownstream(c.Request.Context(), uint(itemID))
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, lineages)
		})

		// 构建血缘图
		api.GET("/graph/:item_id", func(c *gin.Context) {
			itemID, err := strconv.ParseUint(c.Param("item_id"), 10, 32)
			if err != nil {
				c.JSON(400, gin.H{"error": "invalid item_id"})
				return
			}
			maxDepth, _ := strconv.Atoi(c.DefaultQuery("max_depth", "3"))

			graph, err := lineageService.BuildLineageGraph(c.Request.Context(), uint(itemID), maxDepth)
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, graph)
		})

		// 根据 workflow ID 查询
		api.GET("/workflow/:workflow_id", func(c *gin.Context) {
			workflowID := c.Param("workflow_id")
			lineages, err := lineageService.QueryByWorkflowID(c.Request.Context(), workflowID)
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, lineages)
		})
	}

	// 5. 启动服务器
	log.Println("🚀 API Server started at http://localhost:9090")
	log.Println("   Endpoints:")
	log.Println("   - GET /api/lineage/all")
	log.Println("   - GET /api/lineage/upstream/:item_id")
	log.Println("   - GET /api/lineage/downstream/:item_id")
	log.Println("   - GET /api/lineage/graph/:item_id?max_depth=3")
	log.Println("   - GET /api/lineage/workflow/:workflow_id")

	if err := r.Run(":9090"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

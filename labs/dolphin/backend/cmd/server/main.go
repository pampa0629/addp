package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os/exec"

	"github.com/gin-gonic/gin"
)

// OperatorInfo 算子信息
type OperatorInfo struct {
	Code        string                   `json:"code"`
	Name        string                   `json:"name"`
	Category    string                   `json:"category"`
	Description string                   `json:"description"`
	Params      []map[string]interface{} `json:"params"`
	OutputType  string                   `json:"output_type"`
}

// WorkflowNode 工作流节点
type WorkflowNode struct {
	NodeID        string                 `json:"node_id"`
	OperatorCode  string                 `json:"operator_code"`
	Params        map[string]interface{} `json:"params"`
	UpstreamNodes []string               `json:"upstream_nodes"`
}

// WorkflowCreateRequest 工作流创建请求
type WorkflowCreateRequest struct {
	ProjectName  string          `json:"project_name" binding:"required"`
	WorkflowName string          `json:"workflow_name" binding:"required"`
	Nodes        []WorkflowNode  `json:"nodes" binding:"required"`
}

func main() {
	r := gin.Default()

	// 启用 CORS
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 1. 获取所有可用算子列表
	r.GET("/api/operators", getOperators)

	// 2. 创建空间分析工作流
	r.POST("/api/workflows", createWorkflow)

	// 3. 执行单个算子（调试用）
	r.POST("/api/operators/:code/execute", executeOperator)

	r.Run(":8093")
}

// getOperators 获取所有可用算子
func getOperators(c *gin.Context) {
	// 调用 Python 脚本获取算子列表
	cmd := exec.Command("python3", "backend/spatial/operator_registry.py")
	output, err := cmd.Output()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var operators []OperatorInfo
	if err := json.Unmarshal(output, &operators); err != nil {
		c.JSON(500, gin.H{"error": "Failed to parse operators"})
		return
	}

	c.JSON(200, gin.H{
		"operators": operators,
	})
}

// createWorkflow 创建工作流并提交到 DolphinScheduler
func createWorkflow(c *gin.Context) {
	var req WorkflowCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 1. 生成工作流定义文件
	workflowDefJson, err := json.Marshal(req)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to marshal workflow"})
		return
	}

	tmpFile := fmt.Sprintf("/tmp/workflow_%s.json", req.WorkflowName)
	if err := ioutil.WriteFile(tmpFile, workflowDefJson, 0644); err != nil {
		c.JSON(500, gin.H{"error": "Failed to write workflow file"})
		return
	}

	// 2. 调用 DolphinScheduler API 创建工作流
	// TODO: 实现实际的 DolphinScheduler API 调用
	// dolphinClient := NewDolphinClient()
	// workflowID, err := dolphinClient.CreateWorkflow(req)

	c.JSON(200, gin.H{
		"message":      "Workflow created successfully",
		"workflow_id":  "mock_workflow_id_12345",
		"project_name": req.ProjectName,
		"nodes_count":  len(req.Nodes),
	})
}

// executeOperator 执行单个算子（用于测试）
func executeOperator(c *gin.Context) {
	operatorCode := c.Param("code")

	var params map[string]interface{}
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 构造任务配置
	taskConfig := map[string]interface{}{
		"operator": operatorCode,
		"params":   params,
	}

	taskConfigJson, _ := json.Marshal(taskConfig)

	// 执行算子
	cmd := exec.Command(
		"python3",
		"backend/spatial/operator_executor.py",
		string(taskConfigJson),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		c.JSON(500, gin.H{
			"error":  err.Error(),
			"output": string(output),
		})
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		c.JSON(500, gin.H{
			"error":      "Failed to parse result",
			"raw_output": string(output),
		})
		return
	}

	c.JSON(200, result)
}
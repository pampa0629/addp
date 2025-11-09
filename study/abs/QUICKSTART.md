# ABS Quick Start Guide

## 一分钟快速开始

```bash
# 1. 进入目录
cd /Users/pampa/code/addp/study/abs

# 2. 初始化项目
make init

# 3. 配置 API Key
# 编辑 backend/.env，设置你的 Claude API key
echo "CLAUDE_API_KEY=sk-ant-your-key-here" >> backend/.env

# 4. 启动系统
./start.sh

# 5. 打开浏览器
# 访问 http://localhost:5180
```

## 测试示例

启动后，在浮动输入框中尝试以下提示词：

### 示例 1: Hello World 服务器
```
创建一个简单的 HTTP 服务器，监听 8888 端口，访问根路径返回 "Hello from ABS!"
```

### 示例 2: TODO API
```
Create a REST API with these endpoints:
- GET /todos - return a list of todos
- POST /todos - create a new todo
- GET /todos/:id - get a specific todo
- DELETE /todos/:id - delete a todo

Use in-memory storage. Listen on port 9999.
```

### 示例 3: 文件服务器
```
构建一个静态文件服务器，可以浏览当前目录的文件列表，监听 7777 端口
```

## 查看结果

- 实时进度显示在浮动框中
- 所有任务在主面板显示
- 生成的代码在 `backend/workspace/<task-id>/`
- 编译后的程序会自动运行

## 故障排除

### 端口被占用
```bash
# 查看并杀死占用端口的进程
lsof -ti:8090 | xargs kill  # 后端
lsof -ti:5180 | xargs kill  # 前端
```

### API Key 错误
确保你的 API key 正确设置在 `backend/.env` 中

### 依赖未安装
```bash
cd backend && go mod download
cd frontend && npm install
```

## 停止服务

按 `Ctrl+C` 或运行：
```bash
make stop
```

---

更多详情请查看 [README.md](README.md)

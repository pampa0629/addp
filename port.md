统启动成功

  基础设施容器（Docker）

  - PostgreSQL: 运行中，端口 5432
  - Redis: 运行中，端口 6379
  - MinIO: 运行中，端口 9000-9001

  后端服务（开发模式）

  - System Backend: http://localhost:8080 ✅
  - Manager Backend: http://localhost:8081 ✅
  - Meta Backend: http://localhost:8082 ✅
  - Gateway: http://localhost:8000 ✅

  前端服务（开发模式）

  - Portal Frontend: http://localhost:5171 ✅
  - System Frontend: http://localhost:5176 ✅
  - Manager Frontend: http://localhost:5177 ✅

  访问建议

  推荐使用 Portal 入口访问完整系统：
  - Portal（统一入口）: http://localhost:5171

  也可以独立访问各个模块：
  - System 模块: http://localhost:5176
  - Manager 模块: http://localhost:5177
现代数据中台 AI 助手架构设计指南
—— 基于 LangGraph 的动态领域专家（Dynamic Domain Experts）模式
1. 核心设计理念 (Core Philosophy)
本架构旨在解决数据中台场景下业务领域多、API 复杂、逻辑严谨的痛点，摒弃传统的“单体大 Prompt”或“硬编码多 Agent”模式，采用“配置驱动 + 动态实例化”的分层架构。
✅ 三大共识原则
配置预定义，实例动态化：
不在系统启动时加载庞大的 Agent 实例。
只预定义轻量级的“专家映射配置”（Intent -> Skill Path + Prompt Template）。
仅在路由命中时，实时动态构建独立的子 Agent 沙箱。
上下文分层隔离：
主 Agent：负责全局会话记忆（跨领域、长周期），维护对话摘要和关键事实。
子 Agent：负责任务级短期记忆（当前领域内的多步推理），任务结束后立即销毁内部细节，仅返回结论。
Skill 驱动专业化：
利用 SKILL.md 标准化定义领域的 SOP、工具集和业务规则。
子 Agent 本质是 “Skill 文件 + 专属 System Prompt + 临时沙箱” 的运行态组合。
2. 架构分层详解 (Architecture Layers)
2.1 静态资产层 (Static Assets)
存放于文件系统或配置中心，启动时仅读取元数据。
Expert Registry (experts_config.yaml):
定义意图到配置的映射。
包含：skill_path, system_prompt_template, max_steps, allowed_tools_scope。
Skill Packages (skills/<domain>/):
SKILL.md: 核心 SOP、业务规则、Few-shot 示例。
tools.json: 该领域专属的 API 定义。
references/: 领域特有的知识库片段。
2.2 编排控制层 (Orchestration Layer - Main Agent)
基于 LangGraph 的主图 (Main Graph)。
职责：
意图识别与路由：分析用户输入，匹配 Expert Registry。
全局记忆管理：维护 GlobalSessionState（对话摘要、关键实体缓存）。
上下文注入：从全局记忆中提取相关背景，组装成 SubAgentInput。
结果聚合：接收子 Agent 的精简结论，更新全局记忆，生成最终回复。
状态定义 (MainState):
python



class MainState(TypedDict):
    messages: list[BaseMessage]       # 完整对话历史
    global_summary: str               # 全局对话摘要 (长期记忆)
    current_intent: str               # 当前识别的领域意图
    sub_agent_result: str             # 子 agent 返回的结论
2.3 领域执行层 (Execution Layer - Dynamic Sub Agents)
基于 LangGraph 的子图 (Subgraphs)，运行时动态实例化。
职责：
沙箱构建：根据配置读取 Skill 文件，构建独立的 StateGraph。
任务执行：在隔离上下文中执行 ReAct 循环（思考->调用工具->观察）。
自我纠错：依据 Skill 中的 SOP 处理 API 错误、重试逻辑。
输出清洗：丢弃中间日志，仅输出自然语言结论。
状态定义 (SubAgentState):
python



class SubAgentState(TypedDict):
    task_input: str                   # 主 agent 传入的任务描述 + 背景
    internal_messages: list[BaseMessage] # 仅包含当前任务的短期交互
    final_answer: str                 # 最终结论
3. LangGraph 落地实现方案
3.1 核心流程图 (Mermaid)

graph TD
    User[用户输入] --> MainGraph[主图: Main Graph]
    
    subgraph MainGraph [主 Agent (全局指挥官)]
        Router[路由节点: 识别意图] -->|匹配成功 | ConfigLoader[加载专家配置]
        ConfigLoader -->|构建输入包 | SubGraphCaller[调用子图节点]
        SubGraphCaller -->|返回结论 | Summarizer[摘要更新节点]
        Summarizer -->|生成回复 | End[结束]
        
        Router -->|无明确领域 | DirectLLM[直接回答节点]
        DirectLLM --> End
    end
    
    subgraph DynamicSubGraph [动态子图: 领域专家沙箱]
        StartSub[开始] --> LoadSkill[动态加载 Skill & Prompt]
        LoadSkill --> ReActLoop{ReAct 循环}
        ReActLoop -->|思考/行动 | ToolCall[调用领域专属 API]
        ToolCall -->|观察结果 | ReActLoop
        ReActLoop -->|任务完成 | CleanOutput[清洗输出]
        CleanOutput --> EndSub[返回结论]
    end
    
    SubGraphCaller -.->|实例化 | DynamicSubGraph

3.2 关键代码逻辑伪代码
A. 专家配置注册表 (registry.py)
python



EXPERT_REGISTRY = {
    "sales": {
        "skill_path": "./skills/sales-analysis",
        "prompt_template": "你是一名资深销售分析师。请严格遵循以下 SOP:\n{skill_content}",
        "max_steps": 5
    },
    "ops": {
        "skill_path": "./skills/ops-monitor",
        "prompt_template": "你是一名运维专家。遇到报错请按以下流程排查:\n{skill_content}",
        "max_steps": 3
    }
}
B. 动态子图构建工厂 (subgraph_factory.py)
这是核心创新点：不预建图，而是按需构建。
python



def create_dynamic_subgraph(intent: str, input_data: dict) -> StateGraph:
    # 1. 获取配置
    config = EXPERT_REGISTRY.get(intent)
    if not config: raise ValueError("Unknown intent")
    
    # 2. 动态读取 Skill 内容 (按需加载)
    skill_content = load_skill_file(config["skill_path"])
    
    # 3. 组装最终的 System Prompt
    final_system_prompt = config["prompt_template"].format(skill_content=skill_content)
    
    # 4. 构建子图
    workflow = StateGraph(SubAgentState)
    
    # 定义节点：使用动态注入的 prompt 和 tools
    def expert_node(state: SubAgentState):
        # 初始化 LLM，注入动态 prompt 和该领域专属 tools
        llm = ChatModel().bind_tools(load_tools(config["skill_path"]))
        messages = [SystemMessage(content=final_system_prompt)] + state["internal_messages"]
        response = llm.invoke(messages)
        return {"internal_messages": [response]}
    
    workflow.add_node("expert_loop", expert_node)
    # 添加条件边实现 ReAct 循环 (略)
    workflow.set_entry_point("expert_loop")
    
    return workflow.compile()
C. 主图路由与调用 (main_graph.py)
python


def router_node(state: MainState):
    # 识别意图
    intent = detect_intent(state["messages"][-1].content)
    if intent in EXPERT_REGISTRY:
        return {"current_intent": intent}
    return {"current_intent": None}

def call_subgraph_node(state: MainState):
    intent = state["current_intent"]
    
    # 1. 从全局记忆提取相关背景 (主 Agent 的职责)
    context_summary = extract_relevant_context(state["global_summary"], intent)
    
    # 2. 构建子图输入
    sub_input = {
        "task_input": f"{context_summary}\n用户问题: {state['messages'][-1].content}",
        "internal_messages": []
    }
    
    # 3. 【关键】动态实例化并运行子图
    sub_graph = create_dynamic_subgraph(intent, sub_input)
    result = sub_graph.invoke(sub_input)
    
    # 4. 返回清洗后的结论
    return {"sub_agent_result": result["final_answer"]}

# 构建主图
main_workflow = StateGraph(MainState)
main_workflow.add_node("router", router_node)
main_workflow.add_node("execute_expert", call_subgraph_node)
# ... 添加边逻辑
4. 关键职责矩阵 (Responsibility Matrix)
表格

| 功能模块 | 主 Agent (Main Graph) | 领域子 Agent (Dynamic Subgraph) |
| :--- | :--- | :--- |
| 记忆管理 | ✅ 全局长期记忆 (对话摘要、跨轮次实体) <br> ❌ 不保存详细执行日志 | ✅ 任务短期记忆 (ReAct 循环中的思考链) <br> ❌ 任务结束后立即遗忘 |
| Prompt 管理 | ✅ 维护路由逻辑和全局指令 <br> ❌ 不包含具体领域 SOP | ✅ 动态加载 领域 Skill + 专属 Prompt <br> ✅ 包含 Few-shot 和业务规则 |
| 工具调用 | ❌ 不直接调用业务 API | ✅ 全权负责 该领域 API 的调用、重试、参数构造 |
| 错误处理 | ✅ 处理路由失败、子 Agent 超时 | ✅ 处理 API 报错、数据格式异常、逻辑死循环 |
| 输出内容 | ✅ 最终用户回复、全局状态更新 | ✅ 仅返回结论性文本 (隐藏中间过程) |
| 生命周期 | 🔄 常驻内存，贯穿整个会话 | ⚡ 按需创建，用完即毁 (或归档) |



5. 优势总结 (Why This Works)
极致的扩展性：新增一个业务域（如“供应链”），只需添加一个配置文件和一个 Skill 文件夹，无需修改主 Agent 代码。
Token 效率最大化：
主 Agent 上下文永远干净，只存摘要。
子 Agent 仅加载当前任务必要的 Tool 定义和 Skill 内容，避免无关信息干扰。
鲁棒性 (Robustness)：
子 Agent 的沙箱机制防止了某个领域的复杂逻辑（如死循环）搞崩整个会话。
专业的 System Prompt + Skill SOP 确保了领域回答的深度和准确性。
符合人类认知：
像公司运作一样：CEO (主 Agent) 记得所有大事，但具体技术细节交给部门经理 (子 Agent) 临时组建团队解决，解决完只汇报结果。
这套架构是目前构建企业级、多领域、复杂逻辑AI 助手的工业级标准解法
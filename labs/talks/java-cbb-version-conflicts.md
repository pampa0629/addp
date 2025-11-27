# Java 公共 CBB 多版本依赖冲突分析与解决方案

## 问题背景

当一个 Java 公共 CBB（Common Building Block，公共基础库）被不同产品依赖不同版本时，会产生各种冲突问题。本文档详细分析可能的冲突类型及相应的解决方案。

## 可能的冲突类型

### 1. **传递依赖冲突（Transitive Dependency Conflicts）**
```
产品A → CBB v1.0 → Guava 20.0
产品B → CBB v2.0 → Guava 30.0
当产品A和B同时运行 → Guava版本冲突
```

### 2. **API不兼容性冲突**
- 新版本删除/重命名了旧版本的方法
- 方法签名变更（参数、返回值类型改变）
- 类路径变更（包名重构）

### 3. **行为语义冲突**
- 同一方法在不同版本中的实现逻辑变化
- 默认配置项的变更
- 异常处理机制的改变

### 4. **类加载冲突**
```
ClassLoader加载了同名类的不同版本
→ NoSuchMethodError / ClassNotFoundException
→ LinkageError
```

### 5. **状态共享冲突**
- 静态变量在不同版本间的不一致
- 单例模式实现差异导致的多实例问题

## 缓解策略

### 一、依赖管理层面

#### 1. **版本统一策略（推荐优先级：⭐⭐⭐⭐⭐）**
```xml
<!-- 在父POM中统一管理CBB版本 -->
<dependencyManagement>
    <dependencies>
        <dependency>
            <groupId>com.company</groupId>
            <artifactId>common-cbb</artifactId>
            <version>2.5.0</version> <!-- 强制统一版本 -->
        </dependency>
    </dependencies>
</dependencyManagement>
```

**执行计划**：
- 定期组织版本升级评审会议
- 建立统一的版本升级路线图
- 使用 BOM（Bill of Materials）管理依赖

#### 2. **依赖隔离（Maven Shade/Relocation）**
```xml
<plugin>
    <groupId>org.apache.maven.plugins</groupId>
    <artifactId>maven-shade-plugin</artifactId>
    <configuration>
        <relocations>
            <!-- 将CBB依赖的Guava重定向到私有包名 -->
            <relocation>
                <pattern>com.google.common</pattern>
                <shadedPattern>com.company.cbb.shaded.guava</shadedPattern>
            </relocation>
        </relocations>
    </configuration>
</plugin>
```

**适用场景**：
- CBB有强依赖但产品也直接依赖相同库
- 无法短期内统一版本

#### 3. **Optional依赖 + 编译时检查**
```xml
<!-- CBB的pom.xml -->
<dependency>
    <groupId>com.google.guava</groupId>
    <artifactId>guava</artifactId>
    <optional>true</optional> <!-- 不传递给下游 -->
</dependency>
```

```java
// 运行时检查
static {
    try {
        Class.forName("com.google.common.collect.ImmutableList");
    } catch (ClassNotFoundException e) {
        throw new IllegalStateException("需要在运行时环境提供Guava依赖");
    }
}
```

### 二、架构设计层面

#### 4. **语义化版本控制（SemVer）严格遵守**
```
主版本.次版本.修订版本
1.2.3
│ │ └─ PATCH: 向后兼容的Bug修复
│ └─── MINOR: 向后兼容的新功能
└───── MAJOR: 不兼容的API变更
```

**规则**：
- 破坏性变更必须升级主版本
- 标记 `@Deprecated` 至少保留一个大版本
- 提供迁移指南

#### 5. **接口与实现分离**
```java
// cbb-api (稳定的接口层)
public interface DataProcessor {
    Result process(Data input);
}

// cbb-core-v1 (实现v1)
public class DataProcessorV1 implements DataProcessor { }

// cbb-core-v2 (实现v2)
public class DataProcessorV2 implements DataProcessor { }
```

产品可以：
```xml
<dependency>
    <groupId>com.company</groupId>
    <artifactId>cbb-api</artifactId>
    <version>1.0.0</version> <!-- API保持稳定 -->
</dependency>
<dependency>
    <groupId>com.company</groupId>
    <artifactId>cbb-core-v2</artifactId>
    <version>2.3.0</version>
    <scope>runtime</scope>
</dependency>
```

#### 6. **多版本共存（类似OSGi）**
```java
// 使用自定义ClassLoader隔离
public class VersionedCBBLoader {
    private Map<String, ClassLoader> versionLoaders = new HashMap<>();

    public Object loadService(String version, String className) {
        ClassLoader loader = versionLoaders.computeIfAbsent(
            version,
            v -> new URLClassLoader(getCBBJarPath(v))
        );
        return loader.loadClass(className).newInstance();
    }
}
```

⚠️ **警告**：这个方案复杂度高，仅在极端场景使用

### 三、工程实践层面

#### 7. **依赖收敛检查（Enforcer Plugin）**
```xml
<plugin>
    <groupId>org.apache.maven.plugins</groupId>
    <artifactId>maven-enforcer-plugin</artifactId>
    <executions>
        <execution>
            <goals>
                <goal>enforce</goal>
            </goals>
            <configuration>
                <rules>
                    <dependencyConvergence/>
                    <bannedDependencies>
                        <excludes>
                            <!-- 禁止直接依赖已知有问题的版本 -->
                            <exclude>com.company:common-cbb:1.0.0</exclude>
                        </excludes>
                    </bannedDependencies>
                </rules>
            </configuration>
        </execution>
    </executions>
</plugin>
```

#### 8. **自动化兼容性测试**
```java
// 在CBB的CI/CD中执行
@ParameterizedTest
@ValueSource(strings = {"productA-config", "productB-config"})
void testCompatibilityWithProducts(String productConfig) {
    // 使用各产品的依赖配置进行集成测试
    assertDoesNotThrow(() -> {
        runIntegrationTest(productConfig);
    });
}
```

#### 9. **发布变更日志（CHANGELOG.md）**
```markdown
## [2.0.0] - 2025-01-15
### ⚠️ Breaking Changes
- 移除了已废弃的 `LegacyAPI.oldMethod()`
- 迁移指南: 使用 `NewAPI.newMethod()` 替代

### Migration Guide
1. 替换导入语句
2. 更新方法调用
3. 运行 `mvn verify` 确保编译通过
```

#### 10. **渐进式升级策略**
```
阶段1: 发布v2.0，同时维护v1.x分支（6个月）
       ↓
阶段2: 产品A迁移到v2.0（3个月窗口）
       ↓
阶段3: 产品B迁移到v2.0（3个月窗口）
       ↓
阶段4: 停止v1.x维护，仅提供安全补丁
```

## 实施优先级建议

### 短期（1-2周）
1. ✅ 使用 `maven-enforcer-plugin` 检测当前冲突
2. ✅ 建立版本使用情况统计表
3. ✅ 编写当前已知冲突的技术文档

### 中期（1-3个月）
1. ✅ 制定版本统一计划，推动产品团队升级
2. ✅ 对传递依赖使用 Shade Plugin 隔离
3. ✅ 建立 SemVer 规范和发布流程

### 长期（3-6个月）
1. ✅ 重构为接口-实现分离架构
2. ✅ 建立自动化兼容性测试体系
3. ✅ 考虑拆分CBB为更细粒度的模块

## 诊断工具

```bash
# 查看依赖树和版本冲突
mvn dependency:tree -Dverbose

# 分析冲突
mvn dependency:analyze

# 可视化依赖关系
mvn com.github.ferstl:depgraph-maven-plugin:graph
```

## 常见问题排查

### Q1: 如何快速定位是哪个依赖导致的冲突？
```bash
# 查看完整依赖树，包括冲突的版本
mvn dependency:tree -Dverbose -Dincludes=com.google.guava:guava
```

### Q2: 运行时出现 NoSuchMethodError 怎么办？
1. 检查类路径中该类的实际版本：`mvn dependency:build-classpath`
2. 使用 `mvn dependency:tree` 查找版本冲突
3. 通过 `<exclusions>` 排除冲突的传递依赖
4. 使用 `<dependencyManagement>` 强制指定版本

### Q3: 如何验证 Shade Plugin 是否生效？
```bash
# 解压shaded jar并查看类路径
jar -tf target/your-shaded.jar | grep com/google/common
# 应该看到重定向后的路径：com/company/cbb/shaded/guava/...
```

## 最佳实践总结

1. **优先级排序**：版本统一 > 依赖隔离 > 多版本共存
2. **前瞻性设计**：新CBB项目从一开始就采用接口-实现分离
3. **渐进式迁移**：避免强制所有产品同时升级，设置合理的过渡期
4. **自动化检测**：在CI/CD中集成依赖冲突检查
5. **文档先行**：每次破坏性变更都必须提供详细的迁移指南

## 参考资料

- [Maven Dependency Management](https://maven.apache.org/guides/introduction/introduction-to-dependency-mechanism.html)
- [Semantic Versioning 2.0.0](https://semver.org/)
- [Maven Shade Plugin](https://maven.apache.org/plugins/maven-shade-plugin/)
- [Maven Enforcer Plugin](https://maven.apache.org/enforcer/maven-enforcer-plugin/)

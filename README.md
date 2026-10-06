# 古生物修复制备台

古生物修复制备台是一个使用 Go 编写的后端基线，用于记录化石标本、审批修复方案、回填制备进度，并在全部工序完成后办理研究借阅放行。

该服务只使用进程内仓储，便于在本机重复启动和检查。它不依赖数据库、远端接口或其他在线资源。

## 目录

- `cmd/paleo-prep-station`：HTTP 服务入口和监听参数。
- `cmd/workflowcheck`：通过真实 HTTP 处理器执行业务链路的本地验证命令。
- `internal/httpapi`：路由、请求解码、状态码映射和 JSON 输出。
- `internal/station`：跨领域对象的编排、事务边界和业务流程。
- `internal/domain`：标本、方案、进度、子样本、材料去除和放行记录的状态与不变量。
- `internal/repository`：互斥保护的内存仓储和原子提交。
- `internal/contracts`：稳定错误模型和公共请求结构。

## 构建与运行

```bash
go build ./...
go run ./cmd/paleo-prep-station --addr 127.0.0.1:18133
```

服务启动后，`GET /healthz` 返回就绪状态。

## 主要接口

- `GET /specimens` 与 `POST /specimens`
- `GET /specimens/{id}`
- `POST /specimens/{id}/plans`
- `GET /specimens/{id}/plans`
- `GET /plans/{id}`
- `POST /plans/{id}/approval`
- `POST /plans/{id}/progress`
- `POST /plans/{id}/removals`
- `GET /specimens/{id}/subsamples`
- `GET /specimens/{id}/removals`
- `GET /subsamples/{id}`
- `POST /specimens/{id}/release`

## 子样本与材料去除

制备过程中剥离下来的基质、碎片或取样材料通过 `POST /plans/{id}/removals` 登记。每次取样在同一仓储事务中原子生成两条记录：

- 一条不可变的**材料去除记录**：方案、方案内连续步骤序号、直接父级类型与标识、数量、材料类型（`matrix`/`fragment`/`sample`）、操作人和用途；
- 一个具有独立全局编码的**子样本**：根母标本标识、父级类型与父级标识、所属方案与去除步骤、初始数量与当前余量。

子样本仍有余量时可作为后续取样的父级（请求体 `parent_kind` 取 `subsample`，并提供 `parent_id`；缺省为母标本）。祖先链通过不可变父指针保存，始终可回溯到根母标本。母标本的当前库存随去除扣减并在 `GET /specimens/{id}` 的 `stock_quantity` 中保留；登记标本时可用 `initial_quantity` 指定初始库存（缺省为 100）。子样本列表与去除台账分别通过 `GET /specimens/{id}/subsamples` 与 `GET /specimens/{id}/removals` 查询，方案快照 `GET /plans/{id}` 也内嵌其去除序列。

只有方案处于 `active` 且母标本处于 `preparing` 时才能取样：已放行（`available`）、方案已终止（`completed`/`cancelled`）返回 `409 plan_not_accepting_removal`，父级数量不足返回 `409 insufficient_stock`，重复子样本编码返回 `409 duplicate_subsample_code`，父级链断裂或成环分别返回 `subsample_lineage_broken`/`subsample_lineage_cycle`。所有去除在单一互斥事务内串行提交，并发取样得到确定的连续步骤序号，任何校验失败都整体回滚，不会留下孤立的去除记录或子样本。

## 本地验证

每条业务链路都可以独立执行：

```bash
go run ./cmd/workflowcheck --workflow register-specimen
go run ./cmd/workflowcheck --workflow approve-prep-plan
go run ./cmd/workflowcheck --workflow log-preparation
go run ./cmd/workflowcheck --workflow register-subsample
go run ./cmd/workflowcheck --workflow release-for-study
```

这些检查会启动临时 HTTP 处理器并调用真实公共路由。

## 环境变量

项目不需要环境变量，也没有外部服务依赖。

## 测试边界

本基线按要求暂不生成单元测试、测试夹具或端到端测试文件。后续任务阶段会补充红绿验证测试，届时再替换或扩展当前的生产级链路检查。

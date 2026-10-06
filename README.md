# 古生物修复制备台

古生物修复制备台是一个使用 Go 编写的后端基线，用于记录化石标本、审批修复方案、回填制备进度、登记剥离材料与可追溯子样本，并在全部工序完成后办理研究借阅放行。

该服务只使用进程内仓储，便于在本机重复启动和检查。它不依赖数据库、远端接口或其他在线资源。

## 目录

- `cmd/paleo-prep-station`：HTTP 服务入口和监听参数。
- `cmd/workflowcheck`：通过真实 HTTP 处理器执行业务链路的本地验证命令。
- `internal/httpapi`：路由、请求解码、状态码映射和 JSON 输出。
- `internal/station`：跨领域对象的编排、事务边界和业务流程。
- `internal/domain`：标本、子样本、材料去除、方案、进度、终止与放行记录的状态与不变量。
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
- `GET /plans/{id}`
- `POST /plans/{id}/approval`
- `POST /plans/{id}/progress`
- `POST /specimens/{id}/release`
- `POST /materials/{id}/subsamples`（母对象可以是标本或子样本；原子完成去除+子样本登记）
- `POST /materials/{id}/removals`（登记 consumed/discarded 类材料去除）
- `GET /materials/{id}/removals` 与 `GET /materials/{id}/subsamples`
- `GET /subsamples/{id}` 与 `GET /removals/{id}`
- `POST /materials/{id}/termination`（首次 `201`，重复终止幂等返回 `200`）

## 本地验证

每条业务链路都可以独立执行：

```bash
go run ./cmd/workflowcheck --workflow register-specimen
go run ./cmd/workflowcheck --workflow approve-prep-plan
go run ./cmd/workflowcheck --workflow log-preparation
go run ./cmd/workflowcheck --workflow release-for-study
go run ./cmd/workflowcheck --workflow record-subsample
```

这些检查会启动临时 HTTP 处理器并调用真实公共路由。单元与并发测试通过 `go test ./...` 运行；并发取样用例可加 `-race` 校验，去除在仓储互斥下串行提交，成功与超额失败的计数确定。

## 环境变量

项目不需要环境变量，也没有外部服务依赖。

## 测试边界

测试覆盖标本登记、方案与进度、放行、子样本多级溯源、材料去除库存扣减、幂等重放、父子循环防护、并发去除的确定性结果，以及封存（放行/终止）后拒绝取样。失败的去除在仓储事务中整体回滚，不会留下孤立的去除记录或子样本。

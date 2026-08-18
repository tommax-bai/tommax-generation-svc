# tommax-generation-svc

生成任务中心：统一生成任务模型、状态机（PENDING→QUEUED→RUNNING→SUCCEEDED/FAILED/CANCELED）、asynq 调度、模型目录（配置文件）、生成历史。系统核心服务（docs/02 §3.3、docs/05）。

## 本地启动
```bash
# 1. 基础设施（工作区根目录）
docker compose -f ../tommax-infra/compose/docker-compose.yaml up -d
# 2. 依赖服务
(cd ../tommax-model-adapter-svc && make run)
# 3. 本服务（单进程 api+worker）
make run
# 4. 冒烟
make e2e
```
API：`POST/GET /v1/generations`、`POST /v1/generations/{id}:cancel`、`GET /v1/models`；鉴权为 DevAuth（`X-Dev-User` 头，**仅本地**）。

## 例外登记（docs/04 §5：偏离规范必须在此说明）
| 例外 | 原因 | 回收条件 |
|---|---|---|
| HTTP 层手写 chi 路由，未接 Kratos/proto 网关 | 纵向切片轻装起步；REST 契约字段与 tommax-proto generation/v1 对齐 | 服务数 ≥3 或需要 gRPC 对外时统一接线 |
| 数据访问用 pgx 手写 SQL，未用 ent | 表面只有 1 张表，代码生成收益为负 | canvas/media 等多表服务落地时评估 ent 并回改 |
| 手动装配未用 wire | 依赖图尚浅 | 装配代码超过 ~100 行时引入 |
| DevAuth 临时鉴权 | Casdoor 未部署 | 接入 Casdoor JWT 后删除 DevAuth |

负责人：TBD

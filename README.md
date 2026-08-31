# GCP GCE HashedHostID 同步与管理系统

本项目用于演示和管理 Google Cloud Platform (GCP) 在 GCE Instance 上支持的 **`HashedHostID`** 功能。`HashedHostID` 用于标识当前 GCE 实例所在的底层物理宿主机唯一标识。

项目支持自动化发现、多后端持久化存储、动态触发加载接口、实时 Pub/Sub 事件更新（LiveMigration、HostError 重启），并自动将 `HashedHostID` 同步为对应 GKE 集群 Node 的 Kubernetes Label (`hashedhostid: <value>`)。

---

## 目录
- [核心特性](#核心特性)
- [架构设计](#架构设计)
- [配置参数说明](#配置参数说明)
- [快速开始](#快速开始)
  - [本地构建与运行](#本地构建与运行)
  - [Docker 容器构建](#docker-容器构建)
- [HTTP API 接口说明](#http-api-接口说明)
- [事件驱动更新 (Cloud Pub/Sub)](#事件驱动更新-cloud-pubsub)
- [GKE Node 标签同步](#gke-node-标签同步)
- [测试与验证](#测试与验证)

---

## 核心特性

1. **初始化全量/分区扫描 (Initialization Scanner)**
   - 调用 GCP Compute Alpha API 获取指定 GCP 项目与区域下的 GCE 实例列表。
   - 检查实例是否开启 `exposeHostTopology: true` 并提取 `resourceStatus.physicalHostTopology.host`。
   - 自动将 `instance_id` 与 `HashedHostID` 等元数据持久化存储。
2. **多模式存储引擎 (Storage Backend)**
   - **Memory**：基于读写锁（`sync.RWMutex`）的高性能内存存储，轻量开箱即用。
   - **Redis**：支持生产环境高可用部署，支持 Key-Value 与 Set 索引，状态跨节点持久化。
3. **动态触发加载端口 (Dynamic Trigger REST API)**
   - 暴露 `/api/v1/sync` 与 `/api/v1/instances/load` 接口，支持用户/自动化运维工具按需重新扫描全量、指定 Zone 或单个实例。
4. **实时事件消费与增量更新 (Event Consumer)**
   - 消费 GCP Pub/Sub 消息（来自 Cloud Logging Sink 的审计日志），捕获宿主机变更事件：
     - `compute.instances.migrateOnHostMaintenance` (宿主机维护热迁移 LiveMigration)
     - `compute.instances.automaticRestart` / `autorestart` (宿主机故障 HostError 自动重启)
   - 自动重新请求 Alpha API 并更新存储。
5. **GKE Node Label 自动同步 (K8s Node Labeler)**
   - 自动识别以 `gke-` 开头的实例（判定为 GKE 节点）。
   - 通过 Kubernetes API 为对应 Node 打上标签：`hashedhostid: <HashedHostID>`。
6. **全环境运行时鉴权**
   - 支持在 GCE 虚拟机、Cloud Run 或 本地运行。
   - 自动通过 GCP Application Default Credentials (ADC) 或 Metadata Server 获取 OAuth2 Access Token。

---

## 架构设计

```
                    +------------------------------------------+
                    |             GCP Cloud Logging            |
                    | (LiveMigration / AutomaticRestart Logs)  |
                    +--------------------+---------------------+
                                         | Sink
                                         v
+------------------+         +-----------------------+
|  HTTP REST API   |         |    Cloud Pub/Sub      |
|  (/api/v1/sync)  |         | (Subscription worker) |
+--------+---------+         +-----------+-----------+
         |                               |
         +---------------+---------------+
                         |
                         v
             +-----------------------+
             |   Syncer Controller   |
             +-----------+-----------+
                         |
      +------------------+------------------+
      |                  |                  |
      v                  v                  v
+-----------+      +-----------+      +-----------+
| GCE Alpha |      |  Storage  |      |  K8s API  |
|  REST API |      | Mem/Redis |      |Node Label |
+-----------+      +-----------+      +-----------+
```

---

## 配置参数说明

支持通过命令行 Flag 或环境变量进行配置：

| 命令行参数 | 环境变量 | 默认值 | 描述 |
| :--- | :--- | :--- | :--- |
| `-project` | `GCP_PROJECT` | `""` | GCP Project ID |
| `-zones` | `GCE_ZONES` | `""` | GCE Zone 列表（逗号分隔，如 `us-east1-b,us-east1-d`，留空扫描全部 Zone） |
| `-skip-init` | `SKIP_INIT` | `false` | 是否跳过启动时的初始化扫描 |
| `-store-type` | `STORE_TYPE` | `memory` | 存储介质：`memory` 或 `redis` |
| `-redis-host` | `REDIS_HOST` | `127.0.0.1` | Redis 主机 IP 或域名 |
| `-redis-port` | `REDIS_PORT` | `6379` | Redis 端口 |
| `-redis-password` | `REDIS_PASSWORD` | `""` | Redis 访问密码 |
| `-redis-db` | `REDIS_DB` | `0` | Redis DB 编号 |
| `-pubsub-project` | `PUBSUB_PROJECT` | 与 `-project` 相同 | Pub/Sub 所在 GCP Project |
| `-pubsub-topic` | `PUBSUB_TOPIC` | `""` | Pub/Sub Topic 名称 |
| `-pubsub-subscription` | `PUBSUB_SUBSCRIPTION` | `""` | Pub/Sub Subscription 名称 |
| `-host` | `HTTP_HOST` | `0.0.0.0` | HTTP 服务监听地址 |
| `-port` | `HTTP_PORT` | `8080` | HTTP 服务监听端口 |
| `-kubeconfig` | `KUBECONFIG` | `""` | Kubeconfig 路径（未提供则自动尝试 In-Cluster 认证） |
| `-k8s-dry-run` | `K8S_DRY_RUN` | `false` | 是否开启 K8s 打标 Dry-Run 模式（只打印日志不实际调用） |

---

## 快速开始

### 本地构建与运行

```bash
# 1. 下载依赖
go mod download

# 2. 本地编译
go build -o bin/hosthashedid cmd/server/main.go

# 3. 运行（内存存储，跳过初始扫描）
./bin/hosthashedid -skip-init=true -port=8080

# 4. 运行（指定 GCP 项目与 Pub/Sub）
./bin/hosthashedid \
  -project="my-gcp-project" \
  -zones="us-east1-b,us-east1-d" \
  -pubsub-subscription="gce-host-events-sub" \
  -store-type="redis" \
  -redis-host="127.0.0.1" \
  -redis-port=6379
```

### Docker 容器构建

```bash
# 构建镜像
docker build -t hosthashedid:latest .

# 运行容器
docker run -p 8080:8080 \
  -e GCP_PROJECT="my-gcp-project" \
  -e STORE_TYPE="memory" \
  hosthashedid:latest
```

---

## HTTP API 接口说明

### 1. 动态触发扫描 / 加载
- **Endpoint**: `POST /api/v1/sync` 或 `POST /api/v1/instances/load`
- **请求体 (可选)**:
  ```json
  {
    "project": "my-gcp-project",
    "zone": "us-east1-b",
    "instance": "gke-node-test-1"
  }
  ```
- **示例 (全量扫描)**:
  ```bash
  curl -X POST http://localhost:8080/api/v1/sync
  ```
- **示例 (指定单实例刷新)**:
  ```bash
  curl -X POST "http://localhost:8080/api/v1/sync?zone=us-east1-d&instance=gke-z3demo-z3-22-hdb-a1bfdebb-7qdh"
  ```

### 2. 获取所有实例及 HashedHostID
- **Endpoint**: `GET /api/v1/instances`
- **响应示例**:
  ```json
  {
    "total": 1,
    "items": [
      {
        "instance_id": "2449360396712485978",
        "instance_name": "gke-z3demo-z3-22-hdb-a1bfdebb-7qdh",
        "project_id": "project-kangwe-poc",
        "zone": "us-east1-d",
        "hashed_host_id": "71397112c5ff11f47eaad4988c5c2e99",
        "is_gke": true,
        "updated_at": "2026-08-31T12:00:00Z"
      }
    ]
  }
  ```

### 3. 获取特定实例
- **Endpoint**: `GET /api/v1/instances/{instance_id}`
- **响应示例**:
  ```json
  {
    "instance_id": "2449360396712485978",
    "instance_name": "gke-z3demo-z3-22-hdb-a1bfdebb-7qdh",
    "project_id": "project-kangwe-poc",
    "zone": "us-east1-d",
    "hashed_host_id": "71397112c5ff11f47eaad4988c5c2e99",
    "is_gke": true,
    "updated_at": "2026-08-31T12:00:00Z"
  }
  ```

### 4. 状态与健康检查
- `GET /healthz`: 返回 `{"status": "healthy"}`
- `GET /api/v1/status`: 返回当前系统状态与运行统计（扫描次数、事件处理数、已同步 GKE 节点数）。

---

## 事件驱动更新 (Cloud Pub/Sub)

在 GCP Cloud Logging 中设置日志路由器 (Log Sink) 导出至 Cloud Pub/Sub Topic：
```
logName:"logs/cloudaudit.googleapis.com%2Fsystem_event"
(protoPayload.methodName="compute.instances.migrateOnHostMaintenance" OR protoPayload.methodName="compute.instances.automaticRestart")
```

当收到事件后，服务会自动从 `protoPayload.resourceName` 提取实例信息，重新请求 GCE Alpha API 抓取最新 HashedHostID 并更新存储与 K8s 标签。

---

## GKE Node 标签同步

当实例名称以 `gke-` 开头时，系统将使用 `k8s.io/client-go` 对对应 Node 进行原子更新：
```json
{
  "metadata": {
    "labels": {
      "hashedhostid": "71397112c5ff11f47eaad4988c5c2e99"
    }
  }
}
```

---

## 测试与验证

运行所有单元测试：
```bash
go test -v ./...
```

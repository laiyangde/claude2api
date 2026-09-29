# Claude2API 部署文档

> 当前维护仓库：https://github.com/laiyangde/claude2api 。当前部署支持代理检测、出口 IP、备注、启停与账号独立出口，并保留统一扩展思考、`effort` 默认 `low`、4 个基础模型 ID 及旧 `-thinking` 名称兼容。下文第 1–9 节保留首次部署记录；fork 更新方式见第 10 节；最新部署记录见第 13 节。

部署日期：2026-09-29。服务器：`192.168.28.61`，CentOS 7 / x86_64。

**当前状态（21:46）：代理检测与启停版本已通过 Git 提交、推送和服务器拉取流程部署，容器健康。现有 1 个账号、1 个 API Key、1 个代理及绑定关系、运行配置均保留。真实模型调用未在本次发布中执行。**

## 1. 访问地址与凭据

| 项目 | 实际配置 |
| --- | --- |
| 管理后台 / 账号池入口 | http://192.168.28.61:8787/ |
| OpenAI 兼容 Base URL | `http://192.168.28.61:8787/v1` |
| Anthropic 兼容 Base URL | `http://192.168.28.61:8787` |
| 容器名称 | `claude2api` |
| 部署目录 | `/opt/claude2api` |
| 生效的 Compose 文件 | `/opt/claude2api/compose.deploy.yaml` |
| 管理密码 | 已按用户指定值更新，见服务器凭据文件 |
| 初始 API Key | 已创建，名称为 `default-client`，见服务器凭据文件 |

通过 SSH 查看记录的管理密码和 API Key：

```bash
ssh root@192.168.28.61
python3 -m json.tool /opt/claude2api-secrets/credentials.json
```

`admin_password` 用于网页登录，`api_key` 用于客户端调用。登录页面直接填写管理密码，无需用户名。凭据文件权限为 `600`，所在目录权限为 `700`；实际密钥不写入项目文档。

本次已按用户要求更新管理密码，并同步凭据文件；已验证新密码登录成功、旧密码失效，现有 API Key 仍可用。以后在后台修改管理密码时，以 `/opt/claude2api/config.yaml` 中的 `admin_password` 为准，凭据文件不会自动更新。API Key 可在后台「密钥管理」中查看或重新创建。

## 2. 部署版本与环境

| 项目 | 值 |
| --- | --- |
| 源码仓库 | https://github.com/basketikun/claude2api |
| 源码提交 | `b8992cda7ea005b45526d3cbd6da317a8e81b32e` |
| 镜像版本标签信息 | `v1.0.0` |
| 镜像仓库 | `ghcr.io/basketikun/claude2api` |
| 固定镜像摘要 | `sha256:2ffd1620c015faf2b162e31a2ea6f9e7de4637582e016450d8efec4126b38079` |
| Docker | `26.1.4` |
| Docker Compose | `v2.27.1` |
| 数据库 | 内置 SQLite，无需另建 MySQL、PostgreSQL 或 Redis |
| 主机端口绑定 | `192.168.28.61:8787` → 容器 `8787/tcp` |
| 时区 | `Asia/Shanghai`，容器时间已核验为 CST |

镜像 OCI 标签中的源码提交与服务器检出的提交一致。部署使用固定摘要，普通重启不会自动切换到新的 `latest`。

部署前已确认 8787 端口空闲，Docker 开机自启已启用；未调整其他服务配置。服务器 firewalld 未运行，本次没有变更防火墙。服务使用上述内网 IP 和 HTTP 地址，尚未配置域名、HTTPS 或反向代理。

项目功能与接口依据：[上游 README](https://github.com/basketikun/claude2api/blob/b8992cda7ea005b45526d3cbd6da317a8e81b32e/README.md)、[上游配置示例](https://github.com/basketikun/claude2api/blob/b8992cda7ea005b45526d3cbd6da317a8e81b32e/config.example.yaml)、[路由定义](https://github.com/basketikun/claude2api/blob/b8992cda7ea005b45526d3cbd6da317a8e81b32e/internal/router/router.go)。

## 3. 文件布局

```text
/opt/claude2api/
├── .git/
├── cmd/、internal/、web/             上游源码
├── docker-compose.yml              上游原始配置，本次未使用
├── compose.deploy.yaml             本次部署配置
├── config.yaml                     实际运行配置，含管理密码，权限 600
├── data/                           持久化目录，权限 700
│   ├── app.db                      账号、API Key、调用日志
│   ├── app.db-wal                  SQLite 运行时可能存在
│   └── app.db-shm                  SQLite 运行时可能存在
├── deployment-manifest.json         版本、镜像摘要、服务器信息
└── deployment-checks.json           本次接口与持久化验证记录

/opt/claude2api-secrets/
└── credentials.json                管理密码和初始 API Key 记录，权限 600
```

所有运维命令均显式指定 `-f compose.deploy.yaml`。不要直接运行不带 `-f` 的 `docker compose up -d`，否则会使用仓库原始配置。

## 4. 实际部署配置

服务器上的 `compose.deploy.yaml`：

```yaml
services:
  claude2api:
    image: ghcr.io/basketikun/claude2api@sha256:2ffd1620c015faf2b162e31a2ea6f9e7de4637582e016450d8efec4126b38079
    container_name: claude2api
    pull_policy: if_not_present
    restart: unless-stopped
    ports:
      - "192.168.28.61:8787:8787"
    volumes:
      - ./data:/app/data
      - ./config.yaml:/app/config.yaml
      - /usr/share/zoneinfo/Asia/Shanghai:/usr/share/zoneinfo/Asia/Shanghai:ro
    environment:
      TZ: Asia/Shanghai
    healthcheck:
      test: ["CMD", "wget", "-q", "-T", "5", "-O", "/dev/null", "http://127.0.0.1:8787/"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

`config.yaml` 内容如下，密码已脱敏，不能直接用此示例覆盖现有配置：

```yaml
proxy: ""
web_host: 0.0.0.0
web_uc_host: localhost
admin_password: "<当前管理密码>"
status_check_seconds: 21600
retry_count: 0
delete_chat: true
max_history_length: 12000
remove_invalid_account: false
detailed_api_log: false
```

- 出口代理当前为空，使用服务器直连。部署时请求 `https://claude.ai/` 返回 HTTP 302，这只能证明网络可达，不能证明账号或模型可用。
- `detailed_api_log: false` 关闭完整请求和响应内容记录，调用元数据仍保存在 SQLite 中。Docker 日志最多保留 3 个 10 MB 文件，SQLite 调用日志需在后台另行清理。
- `delete_chat: true` 在请求完成后清理上游会话；账号状态巡检间隔为 6 小时，不自动移除失效账号。
- 配置文件需要可写，以支持后台保存设置，因此未添加只读挂载。
- 健康检查只验证本地网页服务，不验证 Claude.ai 账号或模型响应；健康失败本身不会触发 Docker 自动重启，`unless-stopped` 用于进程退出和 Docker 重启后的恢复。
- 本次部署范围为管理后台和 API。Artifacts 网页沙箱仍使用上游默认的 `web_uc_host: localhost`，远程完整沙箱访问尚未配置或验证；启用时需要独立可解析域名及对应反向代理。

首次部署执行了克隆源码、拉取官方镜像、生成随机管理密码、创建以上配置，然后运行：

```bash
cd /opt/claude2api
docker compose -f compose.deploy.yaml config --quiet
docker compose -f compose.deploy.yaml up -d --wait --wait-timeout 90
```

## 5. 导入账号并开始调用

1. 打开管理后台，使用服务器凭据文件中的 `admin_password` 登录。
2. 在「账号管理」→「批量导入」中填写你自己的 Claude.ai `sessionKey`，每行一个，例如 `sk-ant-sid01-...`。
3. 等待账号信息获取完成，确认账号处于可用状态。
4. 使用已创建的 `default-client` API Key，或在「密钥管理」中新建专用 Key。
5. 在后台在线测试页面或通过以下命令验证真实模型响应。

`sessionKey` 是 Claude.ai 网页登录凭据，与本服务的 API Key 不同。无需把它写入本地部署文档。

以下命令在服务器的 Bash 中执行，从凭据文件读取 API Key，不将密钥字面值写入命令历史：

```bash
export CLAUDE2API_KEY="$(python3 -c 'import json; print(json.load(open("/opt/claude2api-secrets/credentials.json"))["api_key"])')"

curl --fail --max-time 20 \
  http://192.168.28.61:8787/v1/models \
  -H "Authorization: Bearer $CLAUDE2API_KEY"

curl --max-time 120 \
  http://192.168.28.61:8787/v1/chat/completions \
  -H "Authorization: Bearer $CLAUDE2API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"Reply with OK."}],"stream":false}'

unset CLAUDE2API_KEY
```

本次 `GET /v1/models` 返回以下 6 个模型标识；列表为服务暴露的模型，并不代表当前账号具有相应权限：

- `claude-sonnet-4-6`
- `claude-sonnet-4-6-thinking`
- `claude-haiku-4-5-20251001`
- `claude-haiku-4-5-20251001-thinking`
- `claude-sonnet-5`
- `claude-sonnet-5-thinking`

其他兼容接口：`POST /v1/responses`、`POST /v1/messages`。API 鉴权支持 `Authorization: Bearer <api_key>`，也支持 `x-api-key: <api_key>`。普通 API Key 不具备管理账号的权限。

## 6. 验证结果

验证时间：2026-09-29 16:59，Asia/Shanghai。

| 验证项 | 实际结果 |
| --- | --- |
| Docker 健康检查 | `healthy` |
| 从部署工作站访问 `/` | HTTP 200 |
| 从部署工作站访问 `/static/app.js` | HTTP 200 |
| 无凭据访问 `/v1/models` | HTTP 401 |
| 无凭据访问 `/api/accounts` | HTTP 401 |
| 错误密码登录 | HTTP 401 |
| 首次部署时的随机管理密码登录 | HTTP 200 |
| API Key 以 Bearer 和 x-api-key 两种方式获取模型 | 均为 HTTP 200，返回 6 个模型标识 |
| 使用普通 API Key 访问管理账号接口 | HTTP 401 |
| 强制重建容器后的管理密码与 API Key | 仍有效，数据库中的 Key 保留 |
| 运行配置 | 完整对话日志已关闭 |
| 当前账号数 | 0 |
| 无账号时发起非流式对话 | HTTP 502，`号池中没有可用账号` |
| 真实模型响应 | 待导入有效 Claude.ai 账号后验证 |

HTTP 502 的空号池响应已核对源码，是当前版本的实际行为。无需为了此错误反复重启容器。

## 7. 日常维护

以下命令均在服务器上执行：

```bash
cd /opt/claude2api

docker compose -f compose.deploy.yaml ps
docker compose -f compose.deploy.yaml logs --tail 100 -f
docker compose -f compose.deploy.yaml restart
docker compose -f compose.deploy.yaml stop
docker compose -f compose.deploy.yaml up -d --wait --wait-timeout 90
```

修改 Compose 或替换配置文件后，使用下面的命令重新挂载并应用配置：

```bash
cd /opt/claude2api
docker compose -f compose.deploy.yaml config --quiet
docker compose -f compose.deploy.yaml up -d --force-recreate --wait --wait-timeout 90
```

检查健康状态、部署版本和数据占用：

```bash
docker inspect claude2api --format '{{.State.Health.Status}}'
cat /opt/claude2api/deployment-manifest.json
cat /opt/claude2api/deployment-checks.json
du -sh /opt/claude2api/data
```

主机端口只绑定 `192.168.28.61`，从宿主机测试也应使用该 IP，不能用宿主机的 `127.0.0.1:8787`。Compose 健康检查中的 `127.0.0.1` 是容器内部地址。

## 8. 备份、升级与恢复

SQLite 使用 WAL。备份时短暂停止本服务并复制整个数据目录，避免只复制运行中的 `app.db` 导致数据不完整。以下操作只针对 Claude2API：

```bash
(
  set -eu
  umask 077
  cd /opt/claude2api
  backup_dir="/opt/claude2api-backups/$(date +%Y%m%d-%H%M%S)"
  mkdir -p "$backup_dir"
  trap 'docker compose -f compose.deploy.yaml up -d --wait --wait-timeout 90' EXIT
  docker compose -f compose.deploy.yaml stop
  tar -czf "$backup_dir/state.tar.gz" -C /opt \
    claude2api/compose.deploy.yaml \
    claude2api/config.yaml \
    claude2api/data \
    claude2api/deployment-manifest.json \
    claude2api-secrets/credentials.json
  printf 'Backup: %s/state.tar.gz\n' "$backup_dir"
)
```

升级流程：

1. 先执行上述备份。
2. 查看上游变更，选择要升级的明确版本；拉取镜像后记录其摘要与源码提交。
3. 仅更新 `compose.deploy.yaml` 中的 `image`，保留当前端口、挂载、密码和数据库。
4. 执行配置校验及 `up -d --wait --wait-timeout 90`。
5. 验证后台登录、API Key、模型列表及真实对话。成功后更新 `deployment-manifest.json` 和本部署文档。

升级后需要恢复时，先停止服务并保留故障现场，再从选定的备份恢复：

```bash
(
  set -eu
  cd /opt/claude2api
  backup_file=/opt/claude2api-backups/替换为实际时间戳/state.tar.gz
  test -f "$backup_file"
  tar -tzf "$backup_file"
  docker compose -f compose.deploy.yaml stop
  mv /opt/claude2api/data "/opt/claude2api/data.before-restore-$(date +%Y%m%d-%H%M%S)"
  tar -xzf "$backup_file" -C /opt
  docker compose -f compose.deploy.yaml up -d --force-recreate --wait --wait-timeout 90
)
```

恢复会将账号、Key、密码及调用日志回退到备份时点。首次部署的固定摘要已记录在第 2 节，回滚时无需依赖可能变化的 `latest` 标签。

## 9. 常见问题

| 现象 | 排查方式 |
| --- | --- |
| 页面无法打开 | 检查容器是否运行，以及是否访问 `192.168.28.61:8787`；检查客户端到该内网 IP 的连接 |
| API 返回 401 | 使用后台创建的 API Key，核对请求头，不要把 Claude.ai sessionKey 当作客户端 Key |
| 对话返回 502「号池中没有可用账号」 | 导入有效账号，检查账号状态及权限 |
| 导入账号失败 | 检查 sessionKey 是否有效、上游网络及服务日志；需要代理时在后台配置从容器可访问的代理地址 |
| 已配置代理但仍无法连接 | 容器内的 `127.0.0.1` 指向容器本身；宿主机代理应使用容器可访问的宿主机地址和实际监听端口 |
| 重启后配置不符合预期 | 确认使用 `compose.deploy.yaml`，并检查 `config.yaml`、`data/` 的挂载 |
| Artifacts 沙箱异常 | 该部分尚未配置独立域名与反向代理，不能以基础健康检查判断其可用性 |

部署时曾检查旧文档中记录的宿主机 `7890` 代理端口，但实际未监听，因此本次没有填入该代理地址。

## 10. Fork 源码部署与更新

正式部署流程固定为：**本地修改并测试 → 本地 Git 提交 → 推送 GitHub `main` → 服务器从 GitHub 拉取 → 核对提交 → 构建镜像并更新服务 → 验证并记录版本**。

部署仓库为 https://github.com/laiyangde/claude2api.git ，分支为 `main`。本地 `origin` 使用该仓库的 SSH 地址也可以，服务器使用上述 HTTPS 地址。仅执行本地 `git commit` 不会更新服务器能拉取的代码，必须完成 `git push`。

后续部署默认按此流程执行，不再上传未提交的工作区源码包作为正式发布版本。第 11 节记录的源码快照部署是历史例外，不作为后续操作模板。

### 10.1 本地测试、提交并推送

在本地项目目录执行，先确认当前分支为 `main`，并检查本次需要发布的修改：

```bash
git branch --show-current
git status --short
git diff
go test ./... -skip '^(TestOpenAISDKToolCall|TestAnthropicSDKToolCall|TestGetUserInfo|TestSendMessage|TestUploadFile)$' -count=1
```

测试通过后，使用 `git add -p` 选择本次修改；新增文件需要先用 `git add -- 实际文件路径` 明确加入。不要将运行配置、数据库或凭据提交到 Git。

```bash
git add -p
git diff --cached
git commit -m "Describe the deployment changes"
git push origin main
git rev-parse HEAD
```

记录最后输出的完整提交 SHA，作为服务器部署时的 `expected_commit`。推送失败时先解决推送问题，服务器部署必须使用已成功推送的提交。

### 10.2 服务器首次部署

新环境在服务器上从同一仓库克隆源码构建。首次启动前设置 `config.yaml` 中的管理密码：

```bash
git clone https://github.com/laiyangde/claude2api.git
cd claude2api
cp config.example.yaml config.yaml
```

编辑生成的 `config.yaml`，设置管理密码后启动：

```bash
docker compose -f docker-compose.local.yml up -d --build
```

### 10.3 现有服务器拉取、构建并更新

现有服务器使用 `/opt/claude2api/compose.deploy.yaml`，更新时只替换镜像，保留端口、挂载、数据库和现有 `config.yaml`。管理员密码以服务器当前配置为准，不在 Git 中保存，也不要用示例配置覆盖。

通过 `ssh root@192.168.28.61` 登录后，先检查 `git status --short`，处理服务器上已有的源码修改，确保构建使用的源码与目标提交一致。服务器专用的 `Dockerfile.deploy`、Compose 配置及运行数据应保留，不能用清理命令一并删除。

将下面的 `expected_commit` 替换为第 10.1 节记录的完整提交 SHA，再执行：

```bash
set -eu
cd /opt/claude2api
expected_commit="替换为本地已推送的完整提交SHA"
git remote set-url origin https://github.com/laiyangde/claude2api.git
test "$(git branch --show-current)" = "main"
git pull --ff-only origin main
test "$(git rev-parse HEAD)" = "$expected_commit"
revision=$(git rev-parse --short=12 HEAD)
docker build --pull=false -f Dockerfile.deploy -t "claude2api:fork-$revision" .
```

如果服务器拉取后的提交与 `expected_commit` 不一致，先核对版本，不能继续部署其他提交。当前 CentOS 7 服务器使用 `Dockerfile.deploy`，沿用已验证的固定运行时镜像，仅替换从仓库源码编译的 `/app/claude2api`；Go 构建阶段必须通过离线测试。

构建成功后，把 `compose.deploy.yaml` 中 `claude2api` 服务的 `image` 改为本次生成的 `claude2api:fork-<12位提交SHA>`，其余运行配置保持不变，再执行：

```bash
docker compose -f compose.deploy.yaml config --quiet
docker compose -f compose.deploy.yaml up -d --no-build --wait --wait-timeout 90
docker compose -f compose.deploy.yaml ps
```

最后验证容器健康、后台页面、现有 API Key、模型列表和参数校验。更新 `/opt/claude2api/deployment-manifest.json` 与本部署文档，记录实际部署时间、完整 Git 提交 SHA、镜像标签及镜像 ID；正式 Git 发布的 `source_has_local_changes` 应为 `false`。真实模型调用按当次要求由用户或部署人员验证。

模型列表当前应包含 `claude-sonnet-4-6`、`claude-haiku-4-5-20251001`、`claude-sonnet-5`、`claude-sonnet-5-5`，共 4 个 ID；默认模型仍为 `claude-sonnet-4-6`。旧的 `-thinking` 名称作为兼容别名接受。`GET /v1/models` 仅验证服务暴露的名称，真实对话需另行验证。

### 首次 fork 部署验证（2026-09-29，历史记录）

- 已部署源码提交：`2303a9aa18cabb509acc7b075a70cce5600629d7`。
- 运行镜像：`claude2api:fork-2303a9a`，镜像 ID：`sha256:8c021de2425bd98233ff92054b04c6334f296642b95d8400a4d23abdd3a6079d`。
- 服务器通过 HTTPS 从 fork 拉取代码，使用 `Dockerfile.deploy` 构建；Go 依赖源为 `https://goproxy.cn,direct`，运行时沿用首次部署的固定镜像。
- 适配器测试通过：`CGO_ENABLED=0 go test ./internal/adapter -skip 'Test(OpenAI|Anthropic)SDK' -count=1`。两项 SDK 集成测试依赖运行中的 API 服务，在构建容器中连接失败，因此未纳入构建验证。
- 容器健康状态：`healthy`；现有管理员密码登录成功，未覆盖配置文件。
- 现有 API Key 的 Bearer 和 `x-api-key` 鉴权均通过，模型接口返回 8 个 ID，包含 `claude-sonnet-5-5` 及其 `-thinking` 版本。
- 按要求未创建备份；现有数据库和配置挂载保留。真实上游模型响应未验证。

## 11. Effort 与统一扩展思考部署（2026-09-29 18:26，历史快照部署）

本次从本地工作区快照构建，基于提交 `13c44171d81d36aa4dc0c478d502b0722e3c72cf`，包含尚未提交的 effort 与统一思考改动；并非仅部署该提交。服务器原有 Git 检出保留，实际运行源码位置以 `deployment-manifest.json` 为准。

此记录保留当时的实际操作。后续部署应使用第 10 节的本地提交、推送 GitHub、服务器拉取流程，不沿用本节的工作区快照发布方式。

| 项目 | 实际值 |
| --- | --- |
| 运行镜像 | `claude2api:effort-20260929-182319` |
| 镜像 ID | `sha256:78a20dfcd1200dec98e28b144be4d0195d17bb62e85f2a1eae9f778d4ff5cbf5` |
| 源码与构建目录 | `/opt/claude2api-releases/effort-20260929-182319` |
| 源码包 SHA-256 | `1417695fa42190a66e411c1161f637cced8c05443516e7748516d28c50603d1e` |
| Compose 文件 | `/opt/claude2api/compose.deploy.yaml` |
| 上一版镜像 | `claude2api:fork-2303a9a`，仍保留 |

本次行为：

- 上游 completion 请求必带 `effort`，客户端未传或传 `null` 时默认 `low`。
- `/v1/chat/completions` 的 `reasoning_effort`、`/v1/responses` 的 `reasoning.effort` 仅接受 `low`、`medium`、`high`、`xhigh`。
- `/v1/messages` 的 `output_config.effort` 额外接受 `max`，发送上游前转换为 `xhigh`。
- 三个接口均兼容顶层 `effort`；非法值或相互冲突的字段返回 HTTP 400。
- 所有会话在创建前统一设置 `paprika_mode: "extended"`，不再区分思考和非思考模型。

验证结果：

- Docker 构建阶段全部离线测试通过，包括参数默认值、档位校验、`max` 转换、上游请求体和扩展思考设置；跳过需要真实上游或独立 API 服务的集成测试。
- 容器为 `healthy`；从部署工作站访问后台为 HTTP 200，页面包含新参数说明。
- 原有 API Key 的 Bearer 与 `x-api-key` 鉴权均为 HTTP 200，无凭据请求仍为 HTTP 401。
- 模型列表返回 4 个基础 ID；6 项非法 effort / 参数冲突请求均在 API 层返回 HTTP 400。
- 部署前后配置文件摘要、账号身份记录、API Key 记录及挂载路径一致，账号数与 Key 数均为 1。
- 未发送真实 Claude.ai 对话请求，留给用户测试。

已更新 `/opt/claude2api/deployment-manifest.json` 和 `deployment-checks.json`。本次部署目录保存源码包、构建文件、前后验证记录及上一版 Compose 配置；数据库和 `config.yaml` 继续使用原挂载。

如需切回上一版镜像：

```bash
cd /opt/claude2api
cp -p /opt/claude2api-releases/effort-20260929-182319/compose.previous.yaml compose.deploy.yaml
docker compose -f compose.deploy.yaml up -d --no-build --wait --wait-timeout 90
```

## 12. 代理管理部署（2026-09-29 20:34）

本次先将代理管理和上一版已上线的 effort / 统一扩展思考改动提交并推送至 GitHub `main`，服务器从同一仓库拉取并核对完整提交后构建。未使用本地工作区源码包发布。

| 项目 | 实际值 |
| --- | --- |
| 运行源码提交 | `0edbfa6c602c2bbac5f4f81b4427a58819a56fa2` |
| 镜像 | `claude2api:fork-0edbfa6c602c` |
| 镜像 ID | `sha256:cad96517e4b6837fbe070536e3ea076a1942d2825b53bb944e74a44fdeaa7775` |
| 源码目录 | `/opt/claude2api` |
| 构建文件 | `/opt/claude2api/Dockerfile.deploy` |
| 发布记录目录 | `/opt/claude2api-releases/proxies-0edbfa6c602c` |
| Compose 文件 | `/opt/claude2api/compose.deploy.yaml` |
| 上一版镜像 | `claude2api:effort-20260929-182319`，仍保留 |

新增「代理管理」支持维护 HTTP、HTTPS、SOCKS5、SOCKS5H 代理；「账号管理」可切换指定代理或恢复系统统一出口。批量导入每行支持 `sessionKey [代理地址]`，相同地址自动复用，未填代理时跟随系统配置。导入查询、状态刷新、API 请求、镜像请求均使用账号选定的出口。有关联账号的代理不可直接删除。

启动时已完成 SQLite 结构迁移。部署前后核对了配置文件摘要、账号身份记录摘要、API Key 记录摘要和持久化挂载，均一致。原有账号和 Key 数量均为 1，原账号 `proxy_id` 为空，仍使用统一出口；没有添加测试代理或修改真实账号的出口。

验证结果：

- 本地离线测试和服务器 Docker 构建阶段的全部离线测试通过；真实上游及独立 API 服务集成测试按第 10 节排除。
- 容器健康检查为 `healthy`，工作站访问后台与静态脚本均为 HTTP 200，页面包含代理管理和可选代理导入入口。
- 现有管理密码登录正常；管理账号、代理列表接口正常，无凭据访问返回 HTTP 401。
- 原 API Key 的 Bearer 和 `x-api-key` 鉴权均正常，模型列表仍返回 4 个基础 ID；普通 API Key 无权访问代理管理。
- 非法代理协议、错误导入格式、负数代理 ID 返回 HTTP 400，未写入数据；非法 effort 仍返回 HTTP 400。
- 未执行真实 Claude.ai 对话或修改真实账号代理；出口路由已在发布前通过隔离的本地模拟代理与浏览器端到端测试验证。

`deployment-manifest.json` 与 `deployment-checks.json` 已更新。发布记录目录保存构建日志、部署前后摘要及上一版 Compose 配置。运行镜像使用上表中的源码提交；之后仅补充部署文档的提交不改变镜像版本。

如需回滚本次镜像：

```bash
cd /opt/claude2api
cp -p /opt/claude2api-releases/proxies-0edbfa6c602c/compose.previous.yaml compose.deploy.yaml
docker compose -f compose.deploy.yaml up -d --no-build --wait --wait-timeout 90
```

上一版不支持账号独立代理，回滚后所有账号将使用系统统一出口；数据库和代理记录保留。

## 13. 代理检测与启停部署（2026-09-29 21:46）

| 项目 | 实际值 |
| --- | --- |
| 运行源码提交 | `4a6af10098c63958989938ccb808848d715af1d3` |
| 镜像 | `claude2api:fork-4a6af10098c6` |
| 镜像 ID | `sha256:a8d5c446057bae9d05a210dc2f1c266b59cb1546dd7327ce307a7f4cf3bc37fd` |
| 发布记录目录 | `/opt/claude2api-releases/proxy-health-4a6af10098c6` |
| 上一版镜像 | `claude2api:fork-0edbfa6c602c`，仍保留 |

代理管理现展示地址、协议、备注、绑定账号、最近检测、出口 IP 和启用状态。支持单个检测、批量检测启用的代理、查看绑定账号、编辑、启用/停用和删除。使用前自动检测的结果缓存 5 分钟，检测超时为 12 秒，全局最多并发 4 个检测；相同代理的并发检测合并执行。检测不携带账号凭据，通过 HTTPS 访问 ipify 查询出口 IP，不代表 Claude 账号或模型可用。

停用保留账号绑定，阻止新增绑定和导入复用；API 轮询跳过停用或检测不可用的代理，账号刷新和镜像请求不会回退到统一出口。修改地址或重新启用后清空旧检测结果；并发配置修改会使旧检测结果作废。旧代理在数据库迁移后默认为启用。

验证结果：本地及 Docker 构建阶段离线测试、`go vet`、浏览器端到端验证均通过。端到端验证使用隔离探测服务，覆盖缓存与并发合并、备注、绑定详情、批量检测、启停、不可用账号跳过、导入与 API / 镜像出口路由。正式镜像使用默认的 HTTPS 检测地址。上线后页面、代理接口、参数校验、现有管理密码及 API Key 鉴权正常，容器为 `healthy`；账号、Key、代理、绑定关系和配置摘要保持一致。

首次发布校验因 Docker 返回挂载列表顺序变化触发自动回退；确认实际挂载集合一致后，修正校验为按路径排序比较并重新发布成功。未修改数据挂载位置。发布清单与验证结果已保存至 `/opt/claude2api/deployment-manifest.json` 和 `deployment-checks.json`。

如需回滚：

```bash
cd /opt/claude2api
cp -p /opt/claude2api-releases/proxy-health-4a6af10098c6/compose.previous.yaml compose.deploy.yaml
docker compose -f compose.deploy.yaml up -d --no-build --wait --wait-timeout 90
```

上一版保留账号代理选择功能，但不会执行代理检测或遵循停用状态。

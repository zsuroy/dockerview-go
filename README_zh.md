<div align="center">

<!-- markdownlint-disable MD033 -->
<img src="assets/logo.svg" alt="DockerView Go" width="120" />

# DockerView-Go

一款基于 Go 和 bubbletea 构建的精美终端 Docker 容器监控工具，配备华丽的实时 Web 仪表盘。

[![Release](https://img.shields.io/github/v/release/zsuroy/dockerview-go?logo=github)](https://github.com/zsuroy/dockerview-go/releases/latest)
[![CI](https://img.shields.io/github/actions/workflow/status/zsuroy/dockerview-go/ci.yml?label=ci)](https://github.com/zsuroy/dockerview-go/actions/workflows/ci.yml)
[![Downloads](https://img.shields.io/github/downloads/zsuroy/dockerview-go/total?logo=github&label=downloads)](https://github.com/zsuroy/dockerview-go/releases)
[![License](https://img.shields.io/github/license/zsuroy/dockerview-go)](https://github.com/zsuroy/dockerview-go/blob/master/LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](https://react.dev/)
[![Docker](https://img.shields.io/badge/Docker-SDK-2496ED?logo=docker&logoColor=white)](https://docs.docker.com/engine/reference/api/client-lib/)

[English](README.md) | 中文

</div>

## 演示

![DockerView Go Demo](assets/demo.gif)

## 功能特性

- **实时监控**：每秒刷新数据
- **精美终端界面**：基于 [bubbletea](https://github.com/charmbracelet/bubbletea) 和 [lipgloss](https://github.com/charmbracelet/lipgloss) 构建，支持容器启停、重启、日志查看和命令执行
- **实时 Web 仪表盘**：通过 `-server` 参数启用 HTTP 服务器，使用 SSE 实时推送容器数据，提供玻璃拟态风格的 Web 控制台，支持 SVG 迷你图、状态过滤、搜索高亮和 3D 悬停效果
- **Web 容器操控**：直接在 Web 仪表盘中启动、停止、重启容器（仅显示当前会话中通过 dockerview 停止的容器，保持界面整洁）
- **容器健康度评分**：根据 CPU 负载、内存占用、磁盘 I/O 速率、网络吞吐率、重启次数及运行时间动态计算 0-100 的健康度得分，并在顶部展示分组监控面板（健康、警告、危险容器数量），配有霓虹状态灯呼吸效果
- **日志查看器**：在终端或功能强大的 Web 模态框中查看容器日志，支持大小写无关的关键字搜索、日志等级过滤（ALL, DEBUG, INFO, WARN, ERROR）、自定义行数限制、搜索词高亮标色、自动滚动和一键下载日志文件
- **容器命令执行**：在终端（操作面板按 `e`）或 Web 端模态弹窗中直接在运行的容器内部执行 Shell 命令，并清晰展示退出码。Web 端内置常用快捷命令模板（如目录列出、环境变量、磁盘占用、当前用户等），支持标准输出/标准错误标色区分、一键复制输出及 Token 安全防护。
- **Token 安全认证**：控制 API 和日志接口受 Token 保护，自动生成安全密钥，支持访客只读模式，Token 存储于 localStorage
- **多语言支持**：Web 仪表盘支持中英文切换，可在顶部导航栏一键切换语言。
- **移动客户端**：基于 Expo / React Native 的跨平台 App，在手机上镜像仪表盘功能——实时监控、启停/重启操作、日志过滤与命令执行，支持中文与 English。
- **主题切换**：Web 仪表盘支持深色/浅色主题切换（支持自动检测系统主题偏好）。
- **一键 Web 更新**：在页脚版本标牌旁直接触发基于浏览器的自动更新。系统将自动查询 GitHub 发布的最新 Release 版本，智能识别安装方式（`go install` 或 `binary`），安全执行原子替换，并实时推送详细的进度消息。
- **端口映射可视化**：直接在仪表盘卡片上展示容器的端口映射与暴露端口。暴露端口展示为静态标签，已绑定的端口映射则显示为交互式标牌（例如 `8080 → 80/tcp`），点击可直接在浏览器中打开容器对应的 Web 页面。
- **磁盘清理（Prune）**：在 Web 仪表盘中清理未使用的镜像和悬空卷。支持预览候选项目（dry-run）、管理员确认删除，并查看详细的结果摘要和审计日志。访客可预览，删除需要管理员 Token。
- **操作审计中心**：追踪「谁在何时对哪个容器做了什么」。关键写操作（start/stop/restart/exec）会持久化到审计日志，包含操作者身份、来源、时间、容器、结果、耗时和请求上下文。Web 仪表盘提供可搜索的审计视图，支持筛选、分页和 JSON/Markdown 导出。
- **备份快照**：在宿主机重装或版本升级前，将当前容器现场打包为可携带的 zip 归档。支持预览打包计划（不写磁盘）、创建带交接备注的原子归档，并在「备份」标签页中浏览、下载、删除历史快照。默认仅导出运行中的容器，可勾选包含已停止容器。支持通过 `-no-docker` + JSON fixture 离线验证。
- **容器文件传输**：在 Web 仪表盘的「文件」标签页中浏览、上传、下载容器内文件并打包归档。访问被限制在白名单根目录内（默认 `/tmp/dockerview-files`，可通过 `config.yaml` 配置）。上传采用「预览 → 确认」两步流程，覆盖已有文件与创建缺失目录均需显式确认；文件夹可打包为 tar 下载；所有操作均记录审计日志。
- **值班助手（值班问询助手）**：在「Duty」标签页用自然语言提问——「有哪些容器在运行？」、「看看 api 的 ERROR 日志」、「最近谁重启过容器？」——副驾驶会基于实时容器状态、日志和审计记录汇总结论，并附上工具调用轨迹作为证据。涉及容器的变更操作只会被*建议*：先展示影响面，再由管理员 Token 人工确认后才会真正执行；整个过程全部落审计。
- **配置文件与分层优先级**：所有配置按统一链路解析：命令行参数 > `DOCKERVIEW_*` 环境变量 > `config.yaml` > 内置默认值。首次启动自动生成带注释的 `config.yaml` 示例（也可用 `-config-init` 手动生成，绝不覆盖现有文件）；Token 永不写入 YAML（通过 `-token`、`DOCKERVIEW_TOKEN` 或 `token_file` 提供）。
- **状态颜色标识**：运行中为绿色，已停止/退出为红色
- **CPU 告警**：CPU 使用率超过 50% 时红色高亮
- **自动检测**：自动检测 Docker Socket（支持 Unix Socket、WSL、Colima、OrbStack、Podman、Rancher Desktop 等）

## 环境要求

- Go 1.24+
- Docker 守护进程运行中
- 支持真彩色的终端（推荐）

## 安装

### 使用 `go install`

```bash
go install github.com/zsuroy/dockerview-go/cmd/dockerview@latest
```

确保 `$GOPATH/bin`（或 `$HOME/go/bin`）已加入 `PATH`。

### 从源码构建

```bash
git clone https://github.com/zsuroy/dockerview-go.git
cd dockerview-go
make build
./build/dockerview
```

### 快速运行

```bash
go run ./cmd/dockerview/
```

## 使用方法

```bash
./dockerview
```

### 终端快捷键

| 按键        | 操作     |
| ----------- | -------- |
| `↑` `↓`     | 选择容器 |
| `Enter`     | 显示操作 |
| `s`         | 启动容器 |
| `x`         | 停止容器 |
| `r`         | 重启容器 |
| `l`         | 查看日志 |
| `e`         | 执行命令 |
| `q` / `Esc` | 返回/退出 |
| `Ctrl+C`    | 退出程序 |

### Web 仪表盘与服务器模式

启用 HTTP 服务器后，可通过浏览器访问实时 Web 仪表盘：

```bash
# 默认端口 8080
./build/dockerview -server

# 自定义端口
./build/dockerview -server -port 8023

# 设置自定义安全 Token
./build/dockerview -server -token my-secret-token
```

启动后在浏览器中访问 `http://localhost:8080`（或自定义端口）即可打开交互式 Web 控制台。

#### 备份快照

在宿主机重装或迁机前，将当前容器现场打包为可携带的 zip 归档：

```bash
# 默认备份行为（仅运行中的容器）
./build/dockerview -server

# 在快照中包含已停止/退出的容器
./build/dockerview -server -backup-dir /opt/backups -backup-max 20

# 无 Docker 守护进程的离线验证（fixture 驱动）
./build/dockerview -server -no-docker -fixture testdata/backup_fixture.json
```

归档结构：`manifest.json`、`containers.json`、`config/runtime.json`、`summaries/<id>-<name>.json`（脱敏 env）、`README.txt`，以及可选的 `images/*.tar`（开启 `include_images` 时）。敏感环境变量值会被掩码为 `***MASKED***`；不含 Token 和 volume 数据。

#### 容器文件传输

「文件」标签页允许管理员在容器内浏览和传输文件。访问范围被限制在白名单根目录内（默认为容器内的 `/tmp/dockerview-files`）：

```yaml
# config.yaml
files:
  jail_root: /tmp/dockerview-files   # 容器内白名单根目录（绝对路径）
  max_file_bytes: 8388608            # 单次传输上限，8 MiB
  max_archive_bytes: 8388608
  allow_guest_download: false        # 访客（无 Token）默认不可下载
```

- **浏览 / 下载**：列出白名单内任意目录，下载单个文件，或将整个文件夹打包为 tar 归档下载。
- **上传**：选择文件后先预览目标（是否已有同名文件？目录是否缺失？），再确认写入。覆盖已有文件需显式勾选确认；创建缺失目录（包括白名单根目录本身）同样需要显式确认。
- 所有操作均记录审计日志。

#### 值班助手（值班问询助手）

在「Duty」标签页接入 OpenAI 兼容大模型, 用自然语言提问即可完成值班排查: 副驾驶会查询容器列表、拉取容器日志、检索审计记录, 并给出**带证据**的结论(每条结论内联工具调用轨迹)。为「群里丢一句 api 502 了」这类场景设计——谁在跑、日志报什么、有没有人点过重启, 一次问完。

```yaml
# config.yaml
agent:
  enabled: true
  # provider: openai-compatible
  # base_url: https://api.openai.com/v1   # 任意 OpenAI 兼容端点
  # model: gpt-4o-mini                    # 网关场景可填 DeepSeek-v4-flash 等
  # api_key_file: /etc/dockerview/agent_key
```

- **API key 绝不写进 yaml**：用环境变量 `DOCKERVIEW_AGENT_API_KEY`(或 `OPENAI_API_KEY`), 或 `api_key_file` 指向 0600 权限文件。没有 key 时自动进入**演练模式**(脚本回答、不联网)。
- **环境变量可覆盖**: `DOCKERVIEW_AGENT_ENABLED=1`、`DOCKERVIEW_AGENT_BASE_URL=…`、`DOCKERVIEW_AGENT_MODEL=…` 与配置键等效; 旧版平铺写法 `agent_enabled`/`agent_model` 等仍然兼容。
- **动容器必须人工确认**: 副驾驶对 start/stop/restart 等变更操作只做提案(展示影响面), 通过 `POST /api/duty/confirm` 且校验管理员 Token 后才真正执行; 提案与确认均写入审计。
- **工单留痕**: 每次问答都会归档到 `data/db/duty.db`, 可在面板内随时查看。

#### 配置文件

所有配置集中在一处：ConfigRoot（默认 `~/.config/dockerview`，可用 `DOCKERVIEW_CONFIG_DIR` 覆盖）。每项配置的优先级：命令行参数 > `DOCKERVIEW_*` 环境变量 > `config.yaml` > 内置默认值。

```bash
# 在 ConfigRoot 中生成带注释的配置示例并退出（绝不覆盖已有文件）
./build/dockerview -config-init

# 照旧通过环境变量或参数覆盖任意配置
DOCKERVIEW_PORT=9090 ./build/dockerview -server
./build/dockerview -server -port 9090
```

#### 安全与访客模式

- **访客视图（只读）**：任何人无需 Token 即可查看实时监控数据（CPU/内存、网络、磁盘 I/O）
- **认证操控（管理员）**：启停重启容器和查看日志需要安全 Token
- **Token 管理**：
  - 未通过 `-token` 参数或 `DOCKERVIEW_TOKEN` 环境变量指定 Token 时，启动时自动生成 16 字节随机十六进制 Token 并打印到控制台
  - 首次点击管理操作或日志时，弹出安全输入框，输入后 Token 保存至浏览器 `localStorage`
  - 通过自动生成的 URL `http://localhost:8080/?token=<token>` 访问可自动认证并清理地址栏参数

### Docker Socket

DockerView-Go 自动检测 Docker Socket：

- 标准 Docker Socket（`/var/run/docker.sock`）
- Colima（`~/.colima/default/docker.sock`）
- 通过 `DOCKER_HOST` 环境变量指定自定义 Socket

```bash
DOCKER_HOST=unix:///path/to/docker.sock ./dockerview
```

## 移动客户端

DockerView 同时提供跨平台**移动客户端**（Expo / React Native），连接同一个 DockerView-Go 后端服务器，在手机上提供实时监控、容器生命周期操作（启停/重启）、日志过滤与交互式命令执行。

![DockerView 移动端演示](assets/mobile.gif)

### 环境要求

- Node.js 20+
- Expo CLI（`npm install -g expo-cli`）或使用 `npx expo`
- 设备可访问运行中的 DockerView-Go 服务器（Android 模拟器使用 `10.0.2.2`，iOS/Web 使用 `localhost`）

### 安装与运行

```bash
cd mobile
npm install
npx expo start          # 使用 Expo Go / 相机扫码
npx expo start --android
npx expo start --ios
```

在 App 内「设置」页面配置服务器地址与可选的安全 Token。App 支持中文与 English。

### 构建独立安装包

- **本地 Android APK**：GitHub 工作流 `.github/workflows/build-mobile.yml` 会自动预构建原生工程并编译签名 Release APK，作为工作流产物（Artifact）提供下载。
- **云端构建（Android & iOS）**：配置仓库密钥 `EXPO_TOKEN`，配合 `eas.json` 的构建配置（`preview` / `production`）即可通过 EAS 构建。iOS 构建还需在 EAS 项目中配置 Apple 凭证。

```bash
cd mobile
npx eas-cli build --platform android --profile preview   # 可安装的 APK
npx eas-cli build --platform ios --profile production    # App Store / 内测分发
```

## 构建命令

```bash
make build      # 构建到 ./build/dockerview
make install    # 安装到 $GOPATH/bin
make test       # 运行测试
make fmt        # 格式化代码
make vet        # 运行 go vet
make deps       # 下载并整理依赖
make release    # 跨平台构建（macOS、Linux、Windows）
make run        # 构建并运行
make clean      # 清理构建目录
```

## 项目结构

```txt
dockerview-go/
├── cmd/dockerview/           # 主终端应用（bubbletea）
│   ├── main.go               # 入口
│   ├── model.go              # TUI 模型
│   ├── update.go             # 自动更新
│   ├── utils.go              # 工具函数
│   └── version.go            # 版本信息
├── internal/
│   ├── audit/               # 操作审计日志（SQLite 存储）
│   ├── backup/              # 备份快照管理器与打包器
│   ├── config/              # 分层配置解析（CLI > env > yaml > 默认值）
│   ├── filejail/            # 文件传输路径约束与穿越防御
│   ├── files/               # 基于 tar 的容器文件复制引擎（进/出/列表/归档）
│   ├── docker/               # Docker API 客户端与健康度评分
│   ├── server/               # HTTP & SSE 服务器
│   │   ├── server.go         # 服务器逻辑与 API 端点
│   │   └── web/              # 编译后的 Web UI 资源（自动嵌入）
│   └── version/              # 版本辅助
├── frontend/                 # React + TypeScript Web 仪表盘（Vite）
│   ├── src/                  # React 源码（App.tsx、组件、i18n 等）
│   ├── index.html            # Vite 模板
│   └── vite.config.ts        # 构建配置（输出到 internal/server/web）
├── mobile/                   # Expo / React Native 移动客户端
│   ├── app/                  # 页面（仪表盘、设置、关于）
│   ├── components/           # 通用 UI 组件
│   ├── utils/                # API 客户端、i18n（zh.ts / en.ts）、存储
│   ├── app.json              # Expo 配置
│   └── eas.json              # EAS 构建配置
├── .github/workflows/        # CI：ci.yml、release.yml、build-mobile.yml
├── Makefile                  # Go 构建命令
├── go.mod / go.sum           # Go 模块
└── README.md                 # 本文件
```

## 许可证

MIT 许可证 - 详见 [LICENSE](LICENSE) 文件

## 作者

[Suroy](https://suroy.cn)

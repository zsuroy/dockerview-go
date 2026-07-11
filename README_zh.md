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

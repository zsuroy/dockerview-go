# Contributing to dockerview-go

感谢你对 dockerview-go 项目的兴趣！我们欢迎任何形式的贡献。

## 开发环境设置

1. Clone 项目
```bash
git clone https://github.com/zsuroy/dockerview-go.git
cd dockerview-go
```

2. 安装依赖
```bash
make deps
```

3. 运行测试
```bash
make test
```

## 代码检查

使用 Go 内置工具进行代码检查：

```bash
# 格式化代码
make fmt

# 运行 go vet 检查常见错误
make vet
```

## 提交代码

1. 创建分支
```bash
git checkout -b feature/your-feature-name
```

2. 提交更改
```bash
git add .
git commit -m "feat: add your feature"
```

3. 推送分支
```bash
git push origin feature/your-feature-name
```

4. 创建 Pull Request

## 提交信息格式

我们使用 [Conventional Commits](https://www.conventionalcommits.org/) 格式：

- `feat:` 新功能
- `fix:` Bug 修复
- `docs:` 文档更改
- `style:` 代码格式化（不影响代码功能）
- `refactor:` 代码重构
- `test:` 添加或修改测试
- `chore:` 构建过程或辅助工具的更改

## 测试

所有新功能都应该包含测试。

```bash
make test
```

## 构建

```bash
# 本地构建
make build

# 构建所有平台
make release
```

## 问题反馈

如果你发现问题或有建议，请在 [Issues](https://github.com/zsuroy/dockerview-go/issues) 中提出。

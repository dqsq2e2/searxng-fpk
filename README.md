# SearXNG for fnOS

将官方 `searxng/searxng` Docker 镜像打包为飞牛 fnOS FPK-Docker 应用，并提供独立的原生配置管理界面。

## 架构

- 飞牛应用卡片打开 Vue 配置 UI。
- Go 管理服务仅监听 Unix Socket，通过 fnOS 统一网关 `/app/searxng-admin/` 提供页面与 API。
- SearXNG 搜索服务保持原生 `http://NAS_IP:8080` 访问方式。
- 配置与品牌资源持久化到 fnOS 应用配置目录。
- x86_64 与 ARM64 分别生成 FPK，容器均使用官方多架构镜像。

## 目录

```text
admin-ui/       Vue 3 配置前端
admin-server/   Go 管理后端
assets/         应用图标源文件
fpk/            FPK-Docker 包模板
tools/          本地与 CI 构建脚本
.github/        GitHub Actions 工作流
```

## 本地构建

需要 Linux/WSL、Node.js 22、Go 1.25 和 `fnpack` 1.2.3。

```bash
npm --prefix admin-ui ci
npm --prefix admin-ui run build
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -C admin-server -o ../build/searxng-admin-linux-amd64 ./cmd/searxng-admin
./tools/package.sh x86 x86 build/searxng-admin-linux-amd64 admin-ui/dist "$(cat VERSION)" /path/to/fnpack dist
```

GitHub Actions 会自动构建 `x86` 和 `arm` 两个安装包；推送 `v*` 标签时同时创建 GitHub Release。

## 上游镜像

默认固定为 `searxng/searxng:2026.7.10-4abac08de` 对应的 OCI digest，更新版本时需同步修改 `fpk/app/docker/docker-compose.yaml` 并完成回归验证。

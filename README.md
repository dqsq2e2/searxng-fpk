# SearXNG for fnOS

<p align="center">
  <img width="816" height="auto" src="https://github.com/dqsq2e2/searxng-fpk/blob/main/poster.png?raw=true" alt="SearXNG for fnOS">
</p>

将隐私友好的开源元搜索引擎 SearXNG 打包为飞牛 fnOS 原生 FPK-Docker 应用。项目使用官方 `searxng/searxng` 多架构镜像，保留完整的 SearXNG 搜索界面，并额外提供可从飞牛应用卡片直接打开的配置管理 UI。

用户无需手动编辑 YAML，即可管理完整搜索引擎目录、站点品牌、搜索行为、界面偏好、外发代理和品牌图片。配置保存后会自动应用并检查服务健康状态，失败时自动回滚；配置和应用数据支持持久化以及卸载时选择保留。

## 架构

- 飞牛应用卡片打开 Vue 配置 UI。
- Go 管理服务仅监听 Unix Socket，通过 fnOS 统一网关 `/app/searxng-admin/` 提供页面与 API。
- 普通 `admin` 容器不挂载 Docker Socket；独立 `apply-controller` 仅接受本地 Unix Socket 的固定重启请求，避免配置 UI 进程直接拥有 Docker 管理权限。
- SearXNG 搜索服务保持原生 `http://NAS_IP:8080` 访问方式。
- 配置与品牌资源持久化到 fnOS 应用配置目录。
- x86_64 与 ARM64 分别生成 FPK，容器均使用官方多架构镜像。

## 配置管理

- 品牌、搜索、界面、外发代理和官方镜像内全部搜索引擎均可在管理 UI 中反复修改。
- 默认启用国产搜索引擎与 Bing；`chinaso news` 因隐私风险保持锁定关闭。
- 默认使用 Bing 搜索建议与 Google 网站图标解析器。
- 支持上传 Logo/Favicon/PWA 图标，以及导入导出原始 `settings.yml`。
- Wordmark 仅支持 SVG；页面 Logo 仅支持 PNG（推荐 640×110）；浏览器图标同时支持独立 PNG 与 SVG；PWA 图标必须分别为 192×192 和 512×512 PNG。
- 引擎目录直接读取固定官方镜像的默认 `settings.yml`，当前 `2026.7.15` 镜像展示全部 346 个默认引擎及自定义引擎。
- 保存采用 revision 冲突检测、YAML 校验、备份和原子替换，并通过隔离控制器自动重启 SearXNG；健康失败时自动回滚。
- 卸载向导默认保留配置和应用数据，也可由用户明确选择永久清除 settings.yml、品牌资源、备份、缓存和控制数据。

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

默认镜像为 `searxng/searxng:2026.7.15-7b2199ecd`，并固定 OCI 多架构摘要 `sha256:268fdb05efbb7b4fdc5957a20c42389bfb1b1b27b5eddeb98f75ec80c45b960f`。手动拉取可使用 `docker pull searxng/searxng:2026.7.15-7b2199ecd`。

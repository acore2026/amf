# AMF Docker 镜像编包指南

## 前置条件

- Docker 已安装并可运行
- `free5gc/base:latest` 基础镜像已存在（首次需构建）
- AMF 源码位于 `/home/core/free5gc-compose/base/free5gc/NFs/amf/`

## 首次构建基础镜像

```bash
cd /home/core/free5gc-compose
make base
```

此步骤拉取 `golang:1.25.5-trixie` 并安装编译依赖，耗时约 5-10 分钟，后续构建复用缓存。

## 日常编包（推荐方式）

使用 `build-nf.sh` 脚本，利用 Docker volume 持久化 Go 缓存，避免重复下载依赖：

```bash
/home/core/free5gc-compose/script/build-nf.sh amf updatev3
```

### 脚本执行流程

| 步骤 | 说明 | 是否下载依赖 |
|------|------|-------------|
| Step 1 | 在容器中编译 AMF 二进制 | 首次下载，后续从 volume 缓存读取 |
| Step 2 | 从容器拷出 binary/config/cert 到宿主机临时目录 | 否 |
| Step 3 | 用 alpine:3.15 构建最终精简镜像 | 否 |

### 缓存机制

两个 Docker volume 持久化 Go 编译缓存：

| Volume 名 | 容器内挂载点 | 缓存内容 |
|-----------|-------------|---------|
| `go-mod-cache` | `/go/pkg/mod` | Go 依赖包源码（.zip + 解压目录） |
| `go-build-cache` | `/root/.cache/go-build` | Go 编译增量缓存 |

首次编包下载约 60+ 个依赖包（耗时 1-2 分钟），后续编包跳过下载，仅重新编译（约 3 秒）。

### 自定义 tag

```bash
# tag 后缀可自定义
/home/core/free5gc-compose/script/build-nf.sh amf my-custom-tag
# 产出: free5gc/amf:my-custom-tag

# 编包其他 NF
/home/core/free5gc-compose/script/build-nf.sh smf updatev3
```

## 传统方式（不推荐）

使用 Makefile 编包，每次在 Docker 容器内重新下载依赖：

```bash
cd /home/core/free5gc-compose
make amf
# 产出: free5gc/amf-base:latest（包含编译环境的中间镜像）

# 再构建最终镜像
docker build -t free5gc/amf:updatev3 ./nf_amf
```

**缺点**：源码改动后 Docker 层缓存失效，所有依赖重新下载，耗时 5-10 分钟。

## 验证镜像

```bash
# 查看镜像列表
docker images | grep amf

# 运行验证
docker run --rm free5gc/amf:updatev3 ./amf --version
```

## 导出镜像文件

```bash
docker save free5gc/amf:updatev3 -o amf.updatev3
```

加载：

```bash
docker load -i amf.updatev3
```

## 常见问题

### DNS 解析失败

容器内默认 DNS 可能无法解析 `goproxy.cn`，脚本已配置 `--dns 8.8.8.8` 和 `GOPROXY=https://proxy.golang.org,direct`。

### git safe.directory 报错

容器内挂载宿主机源码时 git 会报 dubious ownership，脚本已自动添加 `safe.directory` 配置。

### 清理缓存

如需强制重新下载依赖（例如依赖版本更新）：

```bash
docker volume rm go-mod-cache go-build-cache
```

### 完整重编（源码有改动）

脚本每次都会重新编译二进制。如需强制清除旧二进制再编译：

```bash
docker run --rm \
  -v go-mod-cache:/go/pkg/mod \
  -v go-build-cache:/root/.cache/go-build \
  -v /home/core/free5gc-compose/base/free5gc:/go/src/free5gc \
  -w /go/src/free5gc \
  free5gc/base:latest \
  bash -c "rm -f bin/amf && make amf"
```
# 号池程序在线更新

管理员在后台的版本卡片中点击检查更新，再确认「立即更新」，即可下载我们自己的号池程序、重启并检查启动结果。更新程序包含内嵌前端，不再要求安装宿主机 systemd 更新服务，也不需要 Docker socket。

新版使用固定 GitHub 仓库 `dongyaoa/sub2api-pool` 的号池发行版，不会把官方仓库的 `v0.2.x` 当作号池更新。仅有镜像而没有完整程序更新附件的旧发行版不会被提供为可安装更新。

## 首次启用

**旧镜像必须先更新一次 Docker 镜像，才能获得新的持久化程序入口。** 这一次操作沿用原部署目录、Compose 项目、环境变量和数据卷，不要创建新的数据库或替换原 `.env`：

```bash
cd /原来的部署目录
# 原 sub2api 服务使用 ghcr.io/dongyaoa/sub2api-pool:latest
docker compose pull sub2api
docker compose up -d --no-deps sub2api
docker compose ps
```

如原来使用 `docker-compose`，可以保留同一 Compose v2 命令；原部署有 `-p`、多份 `-f` 或 `--env-file` 时继续带上这些参数。`/app/data` 必须是可写的持久挂载，容器应使用 `restart: unless-stopped` 或 `restart: always`，使更新后的程序退出可以触发容器重启。只执行 `up -d` 不保证拉取新的镜像。

目前在线安装支持官方号池镜像的 Linux amd64 运行环境。直接运行旧程序、自定义 entrypoint、只读数据目录、其他平台或没有重启策略的容器不属于此更新部署方式。

如果之前安装过宿主机更新器，应先停用旧服务，避免两个更新方式同时操作应用：

```bash
sudo systemctl disable --now sub2api-pool-updater
```

旧安装器的管理命令 `sub2api-pool-compose` 会追加固定旧镜像的 overlay。迁移到新入口时，使用原 Compose 配置，将应用的 `image` 明确设为新的号池镜像，按上面的原部署命令重建，并确认环境、端口和数据卷保持一致；不要继续使用固定旧镜像的 overlay。旧宿主机工具的文件暂时保留用于兼容，不再是后台在线更新的前置条件。

## 后台更新过程

1. 点击刷新，检查自己的号池仓库是否有更新版本。只有已公开发布、平台匹配、具有完整清单及程序附件的发行版才可安装。
2. 确认所显示的目标版本。服务重新核验版本、commit、附件大小和 SHA-256，下载到数据目录内的临时文件。
3. 校验成功后，保存前一个程序并原子替换运行文件，持久记录更新任务，然后退出触发容器重启。页面短暂断开是正常现象，恢复后可继续查看状态。
4. 新入口确认当前文件的 SHA-256 确实属于本次更新，再检查新程序是否存活及本机 `/health` 或 `/setup/status` 是否可访问。正常启动后清除待确认标记。
5. 新程序退出或在默认 120 秒内未健康启动时，入口恢复上一个程序并记录失败原因。管理员可在页面查看失败结果，修复原因后重试。

更新过程只替换程序和内嵌页面，**Docker 镜像标签、镜像 digest、系统库和外部资源不变**。因此容器详情仍显示原镜像版本，而后台应用版本会显示更新后的程序版本。系统依赖、安全补丁或外部资源发生变更时，仍需更新 Docker 镜像。

自动恢复只恢复程序文件，**不会回滚数据库迁移**。存在数据库不兼容变更的版本，应按发行说明备份并安排维护。新程序启动失败也可能来自数据库、环境变量或端口配置，需要结合容器日志排查。

## 重启与镜像升级

运行程序保存在 `/app/data/runtime/sub2api`。相同镜像重启或重建容器时，该文件会保留，因此不会退回在线更新前的版本。

入口还保存不可变镜像内 `/app/sub2api` 的 SHA-256。将来拉取并重建为不同程序的 Docker 镜像时，入口以新镜像程序建立基线，并清理旧的程序备份及启动标记；旧持久程序不会覆盖新镜像。原更新任务记录保留，后台会将不再匹配的中断任务标记为失败。

`--version`、`--help`、`--setup` 等命令行模式会直接执行相应程序，不运行健康监督，也不会把待确认更新误标记为成功。自定义 Docker 命令（如 `sh`、`pg_dump`）保持原有行为。

## 文件和配置

| 位置或配置 | 用途 |
| --- | --- |
| `/app/sub2api` | 镜像内不可变基线程序 |
| `/app/data/runtime/sub2api` | 实际运行及在线更新的程序 |
| `/app/data/runtime/sub2api.backup` | 上一次安装前的程序备份 |
| `/app/data/runtime/image.sha256` | 当前 Docker 镜像程序的基线指纹 |
| `/app/data/runtime/update-job.json` | 持久任务状态 |
| `/app/data/runtime/update-pending` | 等待启动验证的新程序 SHA-256 |
| `/app/data/runtime/update-rolled-back` | 启动失败或安装中断原因 |
| `/app/data/runtime/update-recovery-required` | 无法可靠恢复时锁定后续更新，需管理员排查 |
| `POOL_APP_UPDATE_DIR` | 默认 `/app/data/runtime`；自定义时必须仍在可写持久卷内 |
| `POOL_APP_UPDATE_HEALTH_TIMEOUT_SECONDS` | 新程序启动健康验证的秒数，默认 `120` |
| `SERVER_PORT` | 本机健康验证端口，默认 `8080`；自定义服务器端口时需同步设置 |

排查时先查看应用容器日志及后台任务结果，不要删除更新状态或备份掩盖失败原因：

```bash
docker compose ps
docker compose logs --tail 200 sub2api
```

## 发行流程

`Pool Images` 工作流接收版本和完整 commit SHA，对精确提交构建 Linux amd64 镜像，运行镜像检查并等待该提交的 CI 成功。之后发布镜像，从**同一个已测试镜像**提取 `/app/sub2api`，生成以下附件：

- `sub2api-linux-amd64`：包含内嵌前端的程序。
- `pool-update.json`：`schema_version: 1`，以及 `version`、`revision`、`platform`、`asset`、`sha256` 和 `size`。

工作流先创建 `pool-vX.Y.Z.N` 草稿发行版，上传两个附件并核对大小及 GitHub 提供的摘要，全部成功后再公开为号池预发行版。上传失败的草稿不可被在线更新发现；同版本重试只能继续相同提交的草稿，已经公开的发行版禁止覆盖，应增加号池版本号。

CI 同时验证入口的首次启动、同镜像持久化、新镜像覆盖、连续更新、命令行参数、健康成功、未配置状态、启动失败恢复、安装中断和停止信号。发布镜像前，真实 Docker 检查会在随机隔离容器及测试卷内，验证新入口的版本输出、独立命令兼容、实际程序带待确认标记重启后的 HTTP 就绪，以及故意损坏新程序后恢复真实备份和数据保留；测试结束仅删除该测试容器和测试卷。

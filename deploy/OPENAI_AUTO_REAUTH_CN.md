# 号池自动授权镜像上线

本次功能需要同一版本的两个镜像：

- `ghcr.io/dongyaoa/sub2api-pool:<版本>`：号池后端和管理界面。
- `ghcr.io/dongyaoa/sub2api-pool-openai-reauth-worker:<版本>`：后台 Chromium 登录服务，镜像已包含浏览器。

将本目录的 `docker-compose.openai-reauth.yml` 放入现有部署目录，与原 Compose 文件叠加使用。它只修改 `sub2api` 的镜像及自动授权环境变量，并增加 worker；原数据库、Redis、数据卷、端口和其他配置继续由原 Compose 文件提供。适用于仓库的 `docker-compose.yml` 和 `docker-compose.local.yml`，以及服务名为 `sub2api` 的现有部署。

## 配置

在现有 `.env` 中补充以下内容，保留文件中的其他设置：

```dotenv
SUB2API_POOL_VERSION=0.2.7-pool.1
OPENAI_REAUTH_WORKER_TOKEN=<至少32字符、无空白的随机令牌>
TOTP_ENCRYPTION_KEY=<沿用当前部署的64位十六进制加密密钥>
OPENAI_REAUTH_WORKER_CONCURRENCY=1
```

`OPENAI_REAUTH_WORKER_TOKEN` 可用 `openssl rand -hex 32` 生成，两端会由 Compose 注入相同值。`TOTP_ENCRYPTION_KEY` 必须沿用现有值；仅从未配置过密钥的新部署才生成它。升级或重启时更换此密钥会使已加密保存的 2FA 和登录资料无法解密。不要把账号密码或账号 2FA 密钥放入 `.env`，它们在管理页面中配置。

worker 使用非 root 用户，只监听应用容器网络空间内的 `127.0.0.1:8091`，无需开放 8091 或 1455 端口。后端会拒绝向远程明文 HTTP worker 发送凭据；本配置通过共享网络空间满足该要求。

## 更新现有部署

在原部署目录执行，保持原来的项目名和 `.env`。若原来使用了 `-p 项目名` 或额外的 `--env-file`，下列每条命令也应使用相同参数。按现有运维流程备份数据库后升级，后端启动时会自动执行新增表迁移。

原部署使用 `docker-compose.yml` 时：

```sh
docker compose -f docker-compose.yml -f docker-compose.openai-reauth.yml config --quiet
docker compose -f docker-compose.yml -f docker-compose.openai-reauth.yml pull sub2api openai-reauth-worker
docker compose -f docker-compose.yml -f docker-compose.openai-reauth.yml up -d --no-deps --force-recreate sub2api openai-reauth-worker
docker compose -f docker-compose.yml -f docker-compose.openai-reauth.yml ps sub2api openai-reauth-worker
docker compose -f docker-compose.yml -f docker-compose.openai-reauth.yml logs --tail=80 sub2api openai-reauth-worker
```

原部署使用 `docker-compose.local.yml` 时，将上面每个 `-f docker-compose.yml` 替换为 `-f docker-compose.local.yml`。有多个原有 Compose 文件时，保留原顺序，把 `-f docker-compose.openai-reauth.yml` 放在最后。

`--no-deps` 只更新应用和 worker，已有数据库与 Redis 应保持运行。不要为了本功能在其他目录另起一套数据库，也不需要 `down` 或删除数据卷。每次应用容器重建时，一同执行上述包含两个服务的 `up --force-recreate`，让 worker 重新加入新应用容器的网络空间。升级会短暂中断应用请求，进行中的恢复任务会由后端按租约重新接管。

## 验证与使用

两个服务健康后刷新管理页面。在“账号”中：

- 新账号：选择“导入新账号并授权”，填写每行 `邮箱----密码----TOTP密钥`，选择固定代理后提交；空账号列表也有入口。
- 已有账号：点击对应行“配置 2FA”，保存该账号的邮箱、密码和长期 TOTP 密钥。沿用原代理及账号设置，保存时不打断有效授权；401 后先刷新，刷新失效才后台重新登录。
- 状态每 5 秒更新。正常密码与 TOTP 流程自动完成；遇到验证码、邮箱或设备确认会停止并显示原因，可沿用原人工 OAuth 授权入口完成额外验证。

账号代理必须提供固定出口或覆盖登录及调用周期的 sticky session。同一代理地址可能仍由供应商轮换出口 IP；程序无法把轮换代理变成固定 IP。支持 HTTP/HTTPS 代理和无认证 SOCKS5，带用户名密码的 SOCKS5 需使用供应商提供的 HTTP 入口。

若状态接口显示 404，检查运行中的 `sub2api` 是否已更新为同一版本。若提示 worker 未配置或不可用，检查两个服务的健康状态、共享 token 和启动日志。更多运行参数见 [worker 说明](../tools/openai-reauth-worker/README.md)。

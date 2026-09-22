# OpenAI 后台重新授权 worker

独立的 Node.js / Playwright 服务，由 Sub2API 后端发起任务。后台 Chromium 完成常规邮箱、密码、认证器 TOTP 和 Codex OAuth 授权；回调在浏览器请求发出之前拦截，worker 只把授权码和 state 交还后端。后端负责授权码交换、身份校验、加密存储、401 检测、任务重试和恢复调度。

## 启动

生产 Docker 部署可直接使用与主应用同版本的 `ghcr.io/dongyaoa/sub2api-pool-openai-reauth-worker` 镜像，按 [Compose 上线说明](../../deploy/OPENAI_AUTO_REAUTH_CN.md) 叠加 `deploy/docker-compose.openai-reauth.yml`，无需在服务器安装 Node.js 或 Chromium。

需要 Node.js 22+。在本目录运行：

```sh
pnpm install --frozen-lockfile
pnpm exec playwright install --with-deps chromium
export OPENAI_REAUTH_WORKER_TOKEN='<至少32字符的随机令牌，和后端保持一致>'
node src/server.mjs
```

Windows / PowerShell 在同一目录安装依赖和 Chromium 后启动：

```powershell
$env:OPENAI_REAUTH_WORKER_TOKEN='<至少32字符的随机令牌>'
node src/server.mjs
```

独立部署时，在启动本次修改后的后端之前，为后端进程设置相同令牌和 worker 地址。当前 Windows 号池的 `tools/local-pool.ps1 -Action Start` / `-Action RestartBackend` 已负责启动本地 worker、配置两端令牌和浏览器缓存路径；令牌自动保存到被 Git 忽略的 `.dev/openai-reauth-worker-token.txt`，无需再手动设置环境变量。脚本继续使用原有固定 TOTP 加密密钥，不要更换。首次使用需按 `LOCAL_DEVELOPMENT.md` 把 Chromium 安装到 `.dev/cache/playwright`。worker 是常驻后台服务，后台任务不依赖管理页面保持打开。

默认仅监听 `127.0.0.1:8091`。`HOST`、`PORT` 可设置监听地址；`OPENAI_REAUTH_WORKER_CONCURRENCY` 为 `1` 或 `2`，默认 `1`。每任务最长 180 秒；客户端断连会终止任务和浏览器。启动时没有有效 token 会直接退出。

Sub2API 后端配置：

```dotenv
OPENAI_REAUTH_WORKER_URL=http://127.0.0.1:8091
OPENAI_REAUTH_WORKER_TOKEN=<与worker相同的至少32字符随机令牌>
TOTP_ENCRYPTION_KEY=<稳定保存的64位十六进制密钥，可用openssl rand -hex 32生成>
```

使用 Docker Compose 启动后端时，还需把两个 `OPENAI_REAUTH_WORKER_*` 变量加入后端服务的 `environment`；仅写入 `.env` 不会自动传入容器。后端必须使用包含此次改动的构建，数据库启动迁移会建立独立的 `openai_auto_reauth` 表。

必须保留固定 `TOTP_ENCRYPTION_KEY`；不要在重启或升级时重新生成，否则已保存的账号登录信息无法解密。账号密码和 TOTP 长期密钥通过管理界面的“自动授权”导入，不放进环境变量、启动命令、源码或聊天。每行格式为 `邮箱----密码----TOTP密钥`，导入时选择该账号固定使用的代理。

新账号从账号页的“导入新账号并授权”进入，列表为空也可使用。选择固定代理和分组，粘贴每行 `邮箱----密码----TOTP密钥`，导入后排队登录；邮箱已存在时提示在对应账号行配置，避免误更新其他账号。已有主 OpenAI OAuth 账号点击对应行“配置 2FA”，按账号 ID 保存邮箱、密码和长期密钥，沿用原代理、分组、令牌与其他设置；保存不会强制重新登录，401 时自动恢复，需要立即执行时可单独点击“立即重新授权”。密码的空格、反斜杠保持原样，每行必须恰好有两个 `----` 分隔符。管理界面接受 Base32 长期密钥，不接受某次六位验证码。

新账号导入后立即排队；已有账号保存凭据仅绑定自动恢复。运行中收到 401 时先刷新已有令牌，确认刷新凭据失效后才进入密码/TOTP 登录。任务期间暂停分配该账号，成功后自动恢复，只更新授权凭据。列表每 5 秒显示绑定、排队、刷新、浏览器登录、令牌交换、成功及失败状态。网络类失败按 30/60 秒退避，最多 3 次；验证码、设备确认、身份不匹配等停止并显示原因。关闭自动授权只取消后台恢复，不会重新启用仍失效的令牌；遇额外验证可用面板的“人工授权”出口，人工浏览器也必须配置该账号的同一固定代理。授权完成仍保留原来的手动停用和其他限流状态。

状态接口 404 表示当前后端尚未包含此功能，需更新并重启；缺少 worker 或密钥会单独显示配置提示。没有账号时返回空列表，不属于加载失败。

Docker 镜像：

```sh
docker build -t sub2api-openai-reauth-worker .
# 后端在名为 sub2api 的容器内时，共享其网络空间即可继续使用 loopback。
docker run -d --name sub2api-openai-reauth-worker \
  --network container:sub2api --init --shm-size=256m \
  --env-file /secure/path/openai-reauth-worker.env \
  --restart unless-stopped sub2api-openai-reauth-worker
```

Docker 基础镜像自带和依赖版本一致的 Chromium。宿主机直接运行后端时，Linux 上可用 `--network host`。不同主机部署必须提供 TLS 反向代理和访问限制；后端拒绝向远程明文 HTTP 地址发送账号密码。不要公开暴露该服务。

## 代理与支持边界

- 强制代理：缺少代理、无效代理或代理故障直接失败，绝不自动直连或切换代理。
- 支持 HTTP、HTTPS（含用户名密码）以及不带认证的 SOCKS5。Chromium 不支持带用户名密码的 SOCKS5；这类配置明确报 `unsupported_proxy`，需要先使用支持认证的 HTTP 代理入口。
- Chromium 的 loopback 绕过也被关闭，WebRTC 非代理 UDP、QUIC 和后台网络被限制。只有固定 `http://localhost:1455/auth/callback` 回调会被本地拦截，不经过代理或监听本地回调端口。
- **同一代理 URL 不代表固定出口 IP。** 代理商必须提供固定出口或足够长的 sticky session；有效期应覆盖登录及后续调用。worker 不会替换代理，也不能把供应商轮换的出口变成固定 IP。
- 认证器使用 RFC 6238 SHA1 / 6 位 / 30 秒，可传 Base32 密钥或兼容的 `otpauth://totp/...`。服务器时钟必须准确。
- 邮箱验证码、验证码挑战、设备确认、通行密钥、账户停用等情况返回固定错误码，停止登录。不会解 CAPTCHA、尝试绕过风控或轮询猜测密码。OAuth 页面结构改变时返回 `login_flow_unsupported`。
- 可选 `workspace_id` 只在页面存在匹配的原工作区选项时自动选择；不能确定原工作区时返回 `workspace_selection_required`，由后端保留原账号身份设置。
- 浏览器使用独立临时上下文，不保存 cookie/profile、截图、录像、trace 或请求日志。worker 不把密码/TOTP写到磁盘；Node.js 不保证字符串内存立即清零，应按凭据处理进程内存及系统转储。

## API

`GET /health` 返回 `{"status":"ok"}`。

`POST /login` 必须带 `Authorization: Bearer <worker token>` 和 `Content-Type: application/json`。请求最多 32 KiB：

```json
{
  "email": "account@example.com",
  "password": "<password>",
  "totp_secret": "<base32-secret>",
  "auth_url": "https://auth.openai.com/oauth/authorize?...",
  "redirect_uri": "http://localhost:1455/auth/callback",
  "proxy_url": "http://user:password@proxy.example:3128",
  "expected_email": "account@example.com",
  "workspace_id": "<optional-original-workspace-id>"
}
```

`auth_url` 只接受项目现有官方 Codex client ID、`response_type=code`、S256 PKCE、唯一有效 state 和固定回调。凭据只输入 `https://auth.openai.com` / `https://chatgpt.com` 的表单。成功响应为 `{"code":"...","state":"..."}`；失败只返回 `{"error_code":"..."}`，无页面原文、异常堆栈或秘密。

常见错误：`busy`（并发占满）、`proxy_unavailable`、`unsupported_proxy`、`login_timeout`、`credentials_rejected`、`captcha_required`、`email_verification_required`、`device_verification_required`、`account_disabled`、`rate_limited`、`workspace_selection_required`、`login_flow_unsupported`。无识别错误统一为 `login_failed`。后端必须在交换 token 后再次核对原账号邮箱和身份；worker 的 `expected_email` 校验不能代替 token 身份校验。

## 验证

```sh
pnpm test
```

测试实际启动无窗口 Chromium，使用合成账号和本地响应夹具执行邮箱→密码→TOTP→授权→回调，不连接真实账号。另有实际本地 HTTP 代理 CONNECT 故障测试，验证断线后不会改用直连。覆盖 RFC TOTP 向量、域名和回调限制、state 错配、恶意表单目的地、额外验证中止、API认证、并发、断连及超时清理。

可用已安装 Chromium 运行测试（仅测试进程使用）：

```powershell
$env:PLAYWRIGHT_TEST_EXECUTABLE_PATH='C:\Program Files\Google\Chrome\Application\chrome.exe'
node --test test/*.test.mjs
```

2026-09-21 本地验证：17 项测试全部通过。只读取了官方初始登录页面（邮箱和继续按钮），未使用真实账号执行登录。因此生产成功率仍依赖官方后续页面、账号状态和代理质量，不能把夹具通过视为真实账号已授权。

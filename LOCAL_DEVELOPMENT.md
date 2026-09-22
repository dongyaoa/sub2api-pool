# 号池版本本地开发

本机的 `sub2api-pool` 与正式版本 `sub2api-custom` 使用独立的进程、端口、数据库、Redis 和应用数据。号池数据固定保存在：

```text
D:\内容\sub2api\sub2api-pool\.dev\data
```

由于 PostgreSQL 在本机的中文路径下初始化失败，已建立 Windows 目录联接 `D:\sub2api-pool-local`，指向本仓库的 `.dev`。脚本通过 `.dev/runtime-root.txt` 读取这个英文入口。目录联接只是访问同一目录的另一个路径，数据的物理位置仍是上面的 D 盘项目目录，并没有第二份数据。如果日后移动项目，需要同步更新该目录联接及入口记录。

## 访问地址与实例配置

| 项目 | 号池版本 | 正式版本保留配置 |
| --- | --- | --- |
| 浏览器入口 | `http://localhost:3001` | `http://127.0.0.1:3000` |
| 后端 | `127.0.0.1:8081` | `127.0.0.1:8080` |
| OpenAI 自动授权 worker | `127.0.0.1:8091` | 不使用 |
| PostgreSQL | `127.0.0.1:5433`，库及用户 `sub2api_pool` | `127.0.0.1:5432`，库 `sub2api` |
| Redis | 独立实例 `127.0.0.1:6380` | `127.0.0.1:6379` |
| 数据目录 | 本仓库 `.dev/data` | `C:\tmp\sub2api-preview` |

号池前端的 `/api`、`/v1` 和 `/setup` 请求均代理到 `8081`。开发服务器只监听本机地址，`3001` 被占用时会报错，避免悄悄改用其他端口。

浏览器访问号池时使用 `localhost`，访问正式版本时使用 `127.0.0.1`，让两站的 OAuth Cookie 也彼此独立。Cookie 不按端口隔离，因此请保持上述不同的主机名；如果测试 OAuth，其回调地址也应使用对应站点的入口地址。

## 启动、查看和停止

在本仓库根目录打开 PowerShell：

```powershell
# 启动 PostgreSQL、Redis、后端和前端；不写 -Action 时默认也是 Start。
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\local-pool.ps1 -Action Start

# 查看本号池实例状态。
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\local-pool.ps1 -Action Status

# 编译并仅重启号池后端，同时启动自动授权 worker；保留数据库、Redis 和前端进程。
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\local-pool.ps1 -Action RestartBackend

# 停止脚本管理的本号池实例，数据继续保留。
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\local-pool.ps1 -Action Stop
```

启动后打开 <http://localhost:3001>。脚本会核对端口和自身管理的进程；如果端口属于其他程序，会报错，需先处理占用。`Stop` 只停止脚本记录并验证归属的进程，不会停止其他仓库的服务，也不负责终止手动启动的 `pnpm dev`。

本地初始管理员为 `pool-admin@sub2api.local`，初始随机密码保存在 `.dev/local-secrets.json`。登录后如果修改密码，以修改后的密码为准。该文件还包含号池独有的数据库、Redis 和应用密钥，应仅留在本机。

## 修改代码

前端修改会由启动脚本启动的 Vite 自动热更新。仓库 CI 和锁文件使用 pnpm 9；本机全局 pnpm 11 对 `package.json` 中 `pnpm.overrides` 的配置读取方式不同，会导致锁文件配置不匹配。因此，本仓库使用 `.dev/runtime/pnpm` 中的 pnpm 9.15.9，并通过专用脚本调用，不修改全局 pnpm。

依赖有变化时，在仓库根目录运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\pnpm-pool.ps1 install --frozen-lockfile
```

该脚本会自动切换到 `frontend`，使用本仓库 `.dev/cache/pnpm`，并保证 package scripts 中嵌套调用的 `pnpm` 仍是本地版本；退出后恢复调用环境。

后端修改后，停止号池实例、重新编译并启动：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\local-pool.ps1 -Action Stop
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\local-pool.ps1 -Action Build
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\local-pool.ps1 -Action Start
```

`Build` 仅编译 Go 后端到 `.dev/bin/sub2api-pool.exe`；正常前端开发不需要构建生产资源。

也可直接执行 `-Action RestartBackend`：先编译新程序，编译通过后才替换当前后端；启动失败会恢复旧程序。前端热更新不会自动更新后端，如果新功能接口返回 404，需执行此操作。

自动授权 worker 随 `Start` 或 `RestartBackend` 在后台启动，随 `Stop` 停止。两端共享的随机令牌首次启动时保存在被 Git 忽略的 `.dev/openai-reauth-worker-token.txt`，无需手动复制或每次设置环境变量；已有 `TOTP_ENCRYPTION_KEY` 保持不变。首次准备环境时需要在 `tools/openai-reauth-worker` 安装依赖，再从仓库根目录安装其专用浏览器：

```powershell
$env:PLAYWRIGHT_BROWSERS_PATH = [IO.Path]::GetFullPath('.dev/cache/playwright')
node tools/openai-reauth-worker/node_modules/playwright/cli.js install chromium
```

账号页可直接“导入新账号并授权”。已有 OpenAI OAuth 账号在对应行“配置 2FA”，保存后等待授权失效自动恢复；需要立即测试时再选择“立即重新授权”。密码与 TOTP 不会回显，也不会进入账号导出。实际出口 IP 由账号固定代理提供。

前端的本机配置位于 `frontend/.env.local`，同时用于开发和构建预览，都会使用号池端口和后端代理。可追踪的模板是 `frontend/.env.pool.example`。`VITE_*` 变量会进入浏览器环境，不能在其中存储数据库密码或其他密钥。

通常直接使用启动脚本即可。如需手动运行前端，应先保证号池后端已运行且 `3001` 空闲，在仓库根目录调用专用脚本：

```powershell
# 前端开发服务器。
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\pnpm-pool.ps1 dev

# 或先构建再预览构建产物。
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\pnpm-pool.ps1 build
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\pnpm-pool.ps1 preview
```

`dev` 与 `preview` 都占用 `3001`，不能与脚本已启动的前端或彼此同时运行。

## 数据、日志与工具位置

| 本仓库相对路径 | 用途 |
| --- | --- |
| `.dev/data/postgres` | 号池 PostgreSQL 数据 |
| `.dev/data/redis` | 号池 Redis 持久化数据 |
| `.dev/data/app` | 号池应用配置和安装状态 |
| `.dev/local-secrets.json` | 本地账号和独立密钥 |
| `.dev/runtime/postgres`、`.dev/runtime/redis`、`.dev/runtime/go`、`.dev/runtime/pnpm` | 本机为号池准备的运行工具 |
| `.dev/runtime-root.txt` | 指向本仓库 `.dev` 的英文目录联接入口 |
| `.dev/bin`、`.dev/cache` | 后端程序和构建缓存 |
| `.dev/logs`、`.dev/run` | 运行日志和进程记录 |

`.dev/` 与 `frontend/.env.local` 已被 Git 忽略。不要删除 `.dev/data` 来解决编译或启动问题；重启和重新编译无需清理数据。两个项目不得共用或互相覆盖数据目录，也不要将正式版本的数据库目录、应用配置或密钥复制到号池目录。

## 新电脑或重新克隆仓库

当前目录的本机运行工具、数据库初始化及秘密配置是在本机单独准备的，不随 Git 提交。生命周期脚本用于管理已经准备好的环境，不是新机器的一键安装器。

新环境需要安装 Node.js，在 `.dev/runtime/pnpm` 准备 pnpm 9，以及准备符合 `backend/go.mod` 的 Go 版本，单独准备 PostgreSQL 和 Redis，初始化号池自己的数据库和密钥，然后通过 `tools/pnpm-pool.ps1` 安装前端依赖，并通过 `tools/local-pool.ps1 -Action Build` 编译后端。前端模板可复制为本机配置：

```powershell
Copy-Item .\frontend\.env.pool.example .\frontend\.env.local
```

这里使用 Windows 本地进程，不依赖 Docker。若日后改用 Docker，应另外配置独立的 Compose 项目名、容器名、端口和数据卷；仓库原有部署文件不承担本机这套环境的隔离配置。

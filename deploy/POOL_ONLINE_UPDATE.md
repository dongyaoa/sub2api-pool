# 号池镜像在线更新

管理员页面检查固定仓库 `ghcr.io/dongyaoa/sub2api-pool` 的最新号池镜像。部署主机安装更新服务后，管理员可确认版本并更新应用容器。更新服务在 Linux 主机上以 systemd 服务运行，只监听 Unix socket；应用容器不挂载 Docker socket，也不获得执行任意 Docker 命令的接口。

目前适用于 Linux amd64、systemd、Docker Engine 和 Compose v2。应用必须是已有 Compose 项目的 `sub2api` 服务，且配置了 Docker 健康检查。Windows 本地开发进程、无 systemd 的容器内环境和其他 CPU 架构只能检查版本，不能使用本安装方式。

## 首次启用

旧版本没有在线更新接口，必须先在终端更新一次到包含本功能的号池版本。**安装器的 bootstrap 仅给当前镜像添加 socket 接入，不升级应用版本。** 如果当前容器没有 `/app/pool-updater`，安装器会停止并提示先完成这次终端升级。

以下例子对应原部署目录 `/www/wwwroot/sub2api`、容器名 `sub2api`。在原目录保留原来的 `.env`、Compose 文件、项目名、端口与数据卷；如原部署使用了 `-p`、多份 `-f` 或 `--env-file`，首次终端升级仍须带上原参数。不要从另一个目录新建同名服务。

```bash
cd /www/wwwroot/sub2api
# 确认原 sub2api 服务的 image 已指向 ghcr.io/dongyaoa/sub2api-pool:latest。
# 仅在首次启用前沿用原部署命令；此处 docker-compose 应为 Compose v2。
docker-compose pull sub2api
docker-compose up -d --no-deps sub2api
docker-compose ps
```

等待容器健康后，从刚拉取的镜像复制安装资源。复制会创建一个不运行的临时容器，不访问模型服务：

```bash
sudo docker pull ghcr.io/dongyaoa/sub2api-pool:latest
installer_container=$(sudo docker create --network none ghcr.io/dongyaoa/sub2api-pool:latest)
sudo docker cp "$installer_container:/app/pool-updater-install" ./pool-updater-install
sudo docker rm "$installer_container"
sudo bash ./pool-updater-install/install-pool-updater.sh --container sub2api
```

安装依赖 `python3`，Docker 位于 `/usr/bin/docker`。安装器固定拉取号池仓库，验证 Linux amd64、版本、commit 与来源标签，并要求版本标签和 `latest` 指向同一 digest/image ID，然后从该不可变镜像复制主机程序。发布中的标签暂时不一致时安装停止，可等发布完成再重试。它不接受自定义镜像仓库、镜像 URL 或安装命令。

安装器从当前容器的 Compose labels 读取完整项目名、工作目录和按顺序排列的配置文件。环境文件优先读取 Compose 的 `environment_file` label；旧部署没有该 label 时，使用记录的工作目录中的 `.env`。安装前用清理过的进程环境渲染 Compose，并逐项核对当前容器的环境、端口和持久挂载。不输出秘密值，也不把当前 shell 的隐式变量当作可重复的部署配置。

所有配置文件、环境文件、Docker/Compose 可执行文件及其父目录必须属于 root，且组与其他用户不可写，不接受符号链接。宝塔等环境中 `/www/wwwroot` 或部署目录可能归 `www` 用户所有，此时安装会拒绝继续。请先由主机管理员核对并调整相关路径的所有权和权限；不要对整个网站目录盲目递归改权限。更新服务具有管理 Docker 的主机权限，因此不能信任可被 Web 用户替换的 Compose 文件或父目录。

通过核对后，安装器写入固定主机配置，运行 `--bootstrap`：使用当前容器同一个 image ID，仅重建 `sub2api` 加入 socket 挂载及 `POOL_UPDATER_SOCKET` 环境变量，等待健康和版本核验。数据库及 Redis 不重建，镜像未升级。成功后启动 systemd 服务；bootstrap 健康检查失败会尝试恢复原声明与原镜像，安装命令仍返回失败。

## 特殊部署使用显式配置

如原部署依赖终端临时环境变量、没有完整 Compose labels、使用不同的环境文件或自定义 Compose 路径，安装器不会猜测。先把原插值变量保存到 root 拥有、权限 `0600` 的环境文件，并用与当前容器 labels 完全一致的路径、项目名和文件顺序准备 JSON：

```json
{
  "compose_files": ["/www/wwwroot/sub2api/docker-compose.yml"],
  "project_name": "sub2api",
  "working_dir": "/www/wwwroot/sub2api",
  "env_files": ["/www/wwwroot/sub2api/.env"],
  "socket_path": "/run/sub2api-pool-updater/updater.sock",
  "state_dir": "/var/lib/sub2api-pool-updater",
  "health_timeout_seconds": 180,
  "socket_gid": 1000,
  "docker_path": "/usr/bin/docker",
  "compose_command": ["/usr/bin/docker", "compose"]
}
```

项目名只是示例，须使用当前容器的 `com.docker.compose.project`。`env_files` 可以为空，但不能丢失原部署所需变量；多个文件保持原顺序。自定义 Compose v2 独立二进制可用 `"compose_command": ["/usr/local/bin/docker-compose"]`，或在自动发现时传 `--compose-bin /usr/local/bin/docker-compose`。不支持只提供 shell 命令字符串。

```bash
sudo chmod 600 /root/pool-updater-config.json
sudo bash ./pool-updater-install/install-pool-updater.sh \
  --container sub2api --config /root/pool-updater-config.json
```

显式配置仍须匹配现有容器的 Compose labels，且通过环境、端口和挂载核验。对于完全缺少 labels 的容器，请先用原配置及正确项目名建立受 Compose 管理的部署；显式配置不绕过身份验证。

默认 socket 组 ID 为镜像中应用进程使用的 `1000`。自行修改容器用户时，需要在可信 JSON 中设置可访问 socket 的实际组 ID。不要把 socket 目录或 Docker socket 改为所有用户可写。

## 安装后的终端管理

**安装后用以下命令替代原来的裸 `docker-compose up -d`：**

```bash
sudo sub2api-pool-compose ps
sudo sub2api-pool-compose up -d
sudo sub2api-pool-compose logs --tail 100 sub2api
```

该管理命令保留已验证的项目名、工作目录、原配置文件列表和环境文件，并追加 `/var/lib/sub2api-pool-updater/compose.override.json`。overlay 保存当前被核验的不可变镜像及 socket 接入，权限为 root-only。原 Compose 文件和用户已有 override 均不改写。

不要再省略该 overlay 直接执行原 `docker-compose up -d`，否则 Compose 可能恢复原镜像标签并丢失 socket 接入。不要编辑或把动态 overlay 复制进原 Compose：后续在线更新会原子替换此文件。需要修改部署配置时，先确认无更新任务，再停止主机服务，用上述管理命令完成变更并核对；路径或原文件列表变化需重新安装配置。

## 更新过程与失败恢复

页面确认的是当时显示的版本和 digest。主机服务在执行前重新核验固定仓库的最新版本，保存旧 image ID，拉取不可变 digest，验证镜像平台和标签，然后只重建 `sub2api`。成功需同时满足容器健康检查与应用报告的版本/commit。更新期间可能短暂断开页面，刷新后可继续查看持久保存的任务状态。

新镜像启动或健康检查失败时，主机服务尝试恢复之前的 image ID，并核验健康和原版本。**自动恢复只涉及应用镜像，不回滚数据库迁移。** 有数据库不向后兼容变更的版本应先按发行说明备份并安排维护，不把镜像恢复当作数据库恢复。

主机更新服务中断或恢复失败时会锁定后续更新，避免重复重建。先检查当前容器及主机日志，再处理实际部署问题：

```bash
sudo systemctl status sub2api-pool-updater
sudo journalctl -u sub2api-pool-updater --since today
sudo sub2api-pool-compose ps
sudo sub2api-pool-compose logs --tail 100 sub2api
```

确认当前应用健康且版本正确后，管理员可在终端解除恢复状态：

```bash
sudo systemctl stop sub2api-pool-updater
sudo /usr/local/libexec/sub2api-pool-updater \
  --config /etc/sub2api-pool-updater/config.json --clear-recovery
sudo systemctl start sub2api-pool-updater
```

`--clear-recovery` 会重新验证当前容器健康与号池版本；它不会自行升级、重建或回滚。没有通过检查时应继续修复原因，不删除 `job.json` 掩盖状态。

## 主机文件与验证

| 位置 | 用途 |
| --- | --- |
| `/usr/local/libexec/sub2api-pool-updater` | 主机更新程序 |
| `/etc/sub2api-pool-updater/config.json` | root-only 固定部署配置 |
| `/etc/systemd/system/sub2api-pool-updater.service` | systemd 服务 |
| `/run/sub2api-pool-updater/updater.sock` | `0660` 的本机 Unix socket，没有 TCP 监听端口 |
| `/var/lib/sub2api-pool-updater/job.json` | 更新状态与前一镜像，原子保存 |
| `/var/lib/sub2api-pool-updater/compose.override.json` | 管理程序追加的镜像/socket overlay |
| `/usr/local/sbin/sub2api-pool-compose` | 保留固定部署参数的终端管理入口 |

CI 对安装配置执行离线测试，并用独立真实 Docker Compose 项目验证 bootstrap 成功、故意健康失败后的原配置恢复，以及镜像 ID、环境变量和数据卷文件的保留。测试容器不连接模型网络；测试结束删除的仅是随机测试项目和其测试卷。

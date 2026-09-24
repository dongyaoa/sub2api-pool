# 上游中心实现契约

管理员私有功能。页面 `/admin/upstreams`，包含上游管理、监控中心、智商监控、OAuth 监控四个 Tab。沿用当前 UI。每 Key 分组和独立监控有最近 60 次宽度自适应状态条，高 22px、铺满所在区域（旧→新；绿正常、黄波动、红错误，缺失灰色），悬停可查看并复制已保存的 HTTP 状态、时间、模型、延迟和错误详情；不暴露原始凭据或未脱敏的上游响应。独立监控保留小卡和同一行紧凑成功率/延迟指标。最后检测时间标签每秒计算已过去秒数，更新标签不会发起探测。

独立监控卡复用 `UpstreamRateBadge` 展示 `target.balance.billing`；缺失显示 `—`，零倍率有效，过期或错误快照保留值并提示状态。「访问网站」链接仅接受绝对 HTTP/HTTPS 地址并使用 `URL.origin`，不把原始 endpoint 路径、userinfo、query、fragment 放进 href、可见文本或 title；新标签页使用 `noopener noreferrer`。无有效源站时隐藏链接。

## HTTP

统一前缀 /api/v1/admin/upstream-center，使用现有 response.Success 包装。

- GET /overview?window=24h|7d|30d 返回 {suppliers: Supplier[], monitors: Target[], summary: FinanceSummary}
- POST /suppliers，PUT /suppliers/:id，DELETE /suppliers/:id。请求 {name, website, notes}；删除软归档，停止其目标调度，保留账目和历史。
- POST /targets，PUT /targets/:id，DELETE /targets/:id。创建/更新用 TargetInput；更新为字段可选，api_key 为空保持不变。删除软归档并关闭账号绑定。
- POST /targets/:id/run，返回 HistoryRecord[]；允许手动检测暂停目标，但不恢复定时任务。
- POST /targets/:id/sync-balance，返回 BalanceSnapshot。
- GET /targets/:id/history?model=&page=1&page_size=50&from=&to= 返回 {items:HistoryRecord[],total,page,page_size}，最新在前，model 为完整字符串。
- POST /models 请求 {target_id?,account_id?,provider,endpoint,api_key?}，返回 {models:string[]}。编辑时可使用既存凭据；导入现有账号时使用 account_id，只读获取其模型且不修改账号能力快照；使用已有凭据时禁止改为其他地址。返回不支持时允许手动输入。
- GET /finance?supplier_id=&target_id=&from=ISO&to=ISO&page=1&page_size=50 返回 {summary:FinanceSummary,items:FinanceRow[],total,page,page_size}。时间范围左闭右开；默认本站时区今日。

## JSON

Supplier: {id,name,website,notes,created_at,updated_at,targets:Target[],finance:FinanceSummary,wallets:BalanceSnapshot[]}

TargetInput: {supplier_id:number|null,name,provider:'openai'|'anthropic'|'gemini',api_mode:'chat_completions'|'responses',endpoint,api_key,source_account_id?:number,models:string[],enabled:boolean,interval_seconds:number,timeout_seconds:number,degraded_threshold_ms:number,account_ids:number[],wallet_ref:string,notes:string}

supplier_id=null 表示独立监控。厂家 Key 分组必须归属厂家。账号绑定可选；可从一个现有 apikey 账号读取 endpoint/key（请求可省略 api_key）。绑定多个账号必须明确为相同上游 endpoint/key，否则拒绝。每个账号仅允许一个活动绑定。新增及批量导入默认模型 gpt-5.6-sol，默认检测间隔 30 秒（30–3600），已有配置保持原值；超时 45 秒（5–45），波动阈值 6000 ms，模型最多8个。钱包标识默认 default，UI 明示同标识代表同一共享钱包，可编辑。

`account_ids` 只适用于厂家 Key 分组的业务归因绑定，独立监控必须为空。独立监控可传 `source_account_id` 一次性从活动、可调度且未因过期暂停的原生 API Key 账号安全复制凭据；仅支持 OpenAI、Anthropic、Gemini，不接受 OAuth、影子或合成测试账号。该参数不得与非空 `api_key`、厂家归属或业务绑定混用；请求地址与协议若显式提供，必须和源账号匹配。服务端加密保存副本，不持久化来源关系、不返回原始 Key 或 `source_account_id`，源账号后续变更不会自动重写监控。编辑再次导入保留未显式修改的模型和间隔。模型获取通过 `account_id` 引用所选源账号，并优先于旧 `target_id`；不得把已有隐藏凭据发送到改动后的地址或协议。

Target: TargetInput 去掉 api_key 和 source_account_id，加 {id,api_key_masked,created_at,updated_at,last_checked_at:string|null,next_check_at:string|null,statistics:ModelStatistics[],balance:BalanceSnapshot|null,finance:FinanceSummary}

ModelStatistics: {model,status:'operational'|'degraded'|'failed'|'error'|'unknown',availability:number|null,availability_7d:number|null,latest_latency_ms:number|null,avg_latency_ms:number|null,p95_latency_ms:number|null,sample_count:number,success_count:number,last_checked_at:string|null,timeline:HistoryRecord[]}

`timeline` 为该模型最近 60 次，旧→新，不受所选统计时间窗裁断。`availability` 为所选窗口内 `(operational+degraded)/采样数`，范围 0–100；`availability_7d` 使用固定最近 7 天窗口，独立于 `window`。没有样本为 `null`，不是 100 或 0。`latest_latency_ms` 来自最近一次检测，包含失败记录；最近记录没有延迟时为 `null`，不能回退到旧记录。`avg_latency_ms`、`p95_latency_ms` 只统计所选窗口的成功样本。

卡片突出 `availability_7d` 和 `latest_latency_ms`，模型切换同时更新两项及时间条。成功率复用渠道监控 `hslForPct` 连续配色；最新延迟按目标 `degraded_threshold_ms` 和 `timeout_seconds` 分别进入黄色、红色，阈值以下绿色，缺失保持中性色。

HistoryRecord: {id,target_id,model,status,latency_ms:number|null,ping_latency_ms:number|null,http_status:number|null,message,checked_at,cost:number|null,cost_source:'unknown'|'estimated'|'reported'}

BalanceSnapshot: {target_id,wallet_ref,kind:'wallet'|'key_quota'|'subscription'|'unsupported'|'unknown',balance:number|null,quota_remaining:number|null,today_used:number|null,total_used:number|null,currency:string,currency_source:'reported'|'sub2api_default',status:'ok'|'error'|'unsupported'|'pending',synced_at:string|null,last_attempt_at:string|null,error:string}。失败保留相同凭据最后成功的金额和 synced_at，last_attempt_at 表示最新尝试；换 Key 后不继承旧金额。

`Supplier.wallets` 中的 `wallet` 以同厂家、同 `wallet_ref`、同币种去重，选取一条完整观测而不相加。金额、来源 Key、同步时间与状态必须来自同一观测。`key_quota` 和 `subscription` 按 Key 隔离，不同币种也不合并；已知共享钱包存在时忽略同 ref 的未知失败占位，具体失败仍留在 `target.balance`。

FinanceSummary: {revenue:number,business_cost:number,monitor_cost:number|null,profit:number|null,request_count:number,total_tokens:number|null,unknown_token_requests:number,account_billed:number,cost_source:'estimated'|'reported'|'mixed'|'unknown',currency:string,from:string,to:string,remote_used:number|null,reconciliation_delta:number|null,unpriced_monitor_count:number}

用户消费为账面消费，非现金收入。监控成本无法估计时为null，profit保守为空，UI显示“成本待补全”，可以额外展示 revenue-business_cost 的业务毛利但不得冒充净利润。同一币种 USD。上游不报告币种时记录标记来源，不跨币种相加。

`request_count` 是同一时间范围内归属此目标的业务账目条数；`total_tokens` 为输入、输出、缓存写入、缓存读取 Token 总和，界面以 M 展示，绝对值达到 `1_000_000_000` 时换为 B。任一账目 Token 未知时合计为 `null`，`unknown_token_requests` 为缺失条数；无业务请求时合计为零。迁移只从仍存在且匹配的 usage 记录回填旧数据，不猜测已清理记录的用量。Key 行右侧财务金额只显示数值，币种保留在 title 与详情中，API 金额单位不变。

`account_billed` 是历史账号计费快照之和，当前等于 `business_cost`：`COALESCE(account_stats_cost,total_cost) * COALESCE(account_rate_multiplier,1)`。明确保存的零倍率不得替换为一；账号配置后续调整不重算旧费用。此字段与用户扣费 `actual_cost` 分离，不再加监控费。

FinanceRow: {id:number,created_at:string,target_id:number,target_name:string,supplier_id:number|null,supplier_name:string,account_id:number|null,group_id:number|null,model:string,request_id:string,revenue:number,business_cost:number,profit:number,billing_type:number,total_tokens:number|null,account_billed:number}

## 智商监控与 OAuth 监控

共用管理员前缀 `/api/v1/admin/intelligence-monitors`，计划 `source_type` 支持 `upstream`、`local_group`、`external`、`openai_oauth`。前端把 `openai_oauth` 单独放在 OAuth Tab，其余三类放在智商监控 Tab。

固定模型 `gpt-6-astra`、思考强度 `high`、提示词 `创建一个 HTML，内容是用 SVG 绘制一个鹈鹕骑自行车的 2D 动画。你不需要任何测试。`。新计划 `enabled=false`，手动执行只排队一次任务，不隐式打开定时。

`interval_seconds` 支持 30–86400 整数秒，默认 3600；`timeout_seconds` 接受 180–900 整数秒，新建默认 900。等待表单按 5 / 10 / 15 分钟点选，编辑非预设的合法旧值时保留原选项；检测间隔使用点选预设和自定义秒数输入，其他选择使用站内无搜索的原生下拉。迁移 249 仅放宽间隔 CHECK；迁移 250 扩大等待时长 CHECK、修改数据库默认值，并将原有 180–300 秒计划提升为 900 秒，不修改开关、下次执行时间及任何已提交运行快照。调度间隔从上一次执行完成后计算，同一计划禁止重叠运行。

OAuth 账号列表查询 `platform=openai&type=oauth&status=active&lite=1`，遍历分页后再排除过载、按策略过期停用、影子及合成账号。保存、启用与执行均由服务端复核 `IsSchedulable()`；失效账号仍可暂停已有计划。无效账号或间隔返回 HTTP 400 / `INTELLIGENCE_MONITOR_INVALID`，metadata.field 分别为 `account_id` 或 `interval_seconds`。

OAuth 计划提交 `account_id` 选择站内已有 OpenAI OAuth 账号，接口强制 Responses。名称取该站内账号名称；运行快照包含本次指定和实际执行的账号信息及可获取的代理信息。复用 OpenAI 网关的 Token 管理、刷新、代理和账号/代理并发控制，不能随机切换账号；不允许固定模型被映射为其他模型，不支持影子或合成测试账号，执行时须可调度。该直接账号执行不单独写入厂家利润账，不能算作普通本站分组记账。

计划返回 `latest_run` 和 `recent_runs`；后者最多 20 条已结束记录，按创建时间与 ID 倒序，用于横版卡片从左新到右旧的作品列表。后台每个计划只物理保留最近 20 条 `succeeded`/`failed` 记录及其作品，包含归档计划；活跃的 `pending`/`running` 不参与删除。完成、租约过期处理及服务启动清理会执行保留规则，因此活动任务期间总记录数可暂时超过 20。

预览使用清理后的无脚本沙箱 iframe。紧凑预览固定逻辑宽度 960，逻辑高度按预览区域比例计算且至少为 600；详情预览保持固定 960×600，两者均等比缩放并居中。尺寸变化和同一记录轮询不会重建 iframe。成功作品右下角常驻耗时角标，优先使用有限非负的 `duration_ms`，缺失时以 `finished_at - started_at` 回退，不包含 `created_at` 到执行开始的排队时间；不足一分钟以秒显示，达到一分钟以分秒显示，无有效耗时则不显示角标。历史详情使用相同格式。查看入口仅在悬停/聚焦时显示，并位于角标上方。排队及绘制中显示本地鹈鹕骑行线稿、描线和状态点动画，不表示上游结果或实际完成进度；减少动态效果设置会停止动画。查看源码与下载使用保留记录的原始 HTML。上游直连、外部地址和 OAuth 智商监控没有单独计入厂家经营费用；保留条数或作品成功都不代表真实付费线路已验收。

## 后端分工与 DB 约定

核心 agent 拥有：service/upstream_center*.go、repository/upstream_center_repo.go、handler/admin/upstream_center_handler.go、242_upstream_center.sql。
财务 agent 拥有：service/upstream_finance*.go、repository/upstream_finance_repo.go、243_upstream_finance.sql；FinanceSummary/BalanceSnapshot/FinanceRow 相关 Go DTO 类型亦由财务定义（UpstreamFinanceSummary / UpstreamBalanceSnapshot / UpstreamFinanceRow）。
主 agent 拥有现有 wire/routes/handler聚合/前端路由与导航/i18n入口接线和集成验证。

核心表名/关键字段固定：
- upstream_suppliers: id,name,website,notes,created_at,updated_at,deleted_at
- upstream_targets: id,supplier_id,name,provider,api_mode,endpoint,api_key_encrypted,models JSONB,enabled,interval_seconds,timeout_seconds,degraded_threshold_ms,wallet_ref,notes,last_checked_at,next_check_at,created_at,updated_at,deleted_at
- upstream_monitor_history: id,target_id,model,status,latency_ms,ping_latency_ms,http_status,message,checked_at,cost NUMERIC NULL,cost_source VARCHAR default unknown（核心可添加 input/output/cache token字段）
- upstream_account_bindings: id,target_id,account_id,valid_from,valid_until（每账号仅一个active，核心在保存目标事务内维护，不变绑定不重开）

绑定时间按 usage_logs.created_at（完成记账时间）归因。改凭据/归属不回写历史。财务 migration 可为 accounts.credentials 的 api_key/base_url 改动添加关闭绑定触发器，删除account同样关闭；解释按记账时有效绑定统计。

构造：NewUpstreamFinanceRepository(db) -> UpstreamFinanceRepository；NewUpstreamFinanceService(repo,encryptor,billing,channels,accounts) -> *UpstreamFinanceService。
财务服务提供：
- Summary(ctx,supplierID *int64,targetID *int64,from,to time.Time) (*UpstreamFinanceSummary,error)
- SyncBalance(ctx,targetID int64) (*UpstreamBalanceSnapshot,error)
- LatestBalance(ctx,targetID int64) (*UpstreamBalanceSnapshot,error)
- Details(ctx,query UpstreamFinanceQuery) (*UpstreamFinancePage,error)，Query/分页DTO财务定义
- SyncDueBalances(ctx) 定时同步尚未归档的到期厂家目标与独立监控，正常约 1 分钟同步余额及倍率；暂停可用性检测不停止同步，且不跟随每轮探测重复同步

核心：NewUpstreamCenterService(repo,encryptor,accountRepo,financeSvc)；NewUpstreamCenterHandler(svc,financeSvc)；独立 runner 或 service.Start/Stop。与财务 agent 主动沟通 API 差异。概要中同步读取 finance；共享 wallet 按 supplier+wallet_ref+currency 去重（明确配置关系，不凭余额数值猜），quota/subscription 按 Key 隔离。

可用性探测禁止泄露 Key 或上游原始错误响应，外部请求复用公网 HTTPS/DNS/dial SSRF 防护并禁止跟随重定向。探测调度独立于站内渠道 V1/V2，多实例用 DB 租约。财务依赖的可用性记录与归属保留；智商任务的授权生成作品适用上述 20 条物理保留及管理员查看规则。

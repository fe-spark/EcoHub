测试版 **v3.1.2-beta.1**，镜像 `ghcr.io/fe-spark/ecohub:v3.1.2-beta.1`。带连字符的预发布标签不会把 `:latest` 指到此版本。正式版仍是下方的 v3.1.1。

中文 | [English](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE_EN.md)

### 升级说明

- 测试安装：在安装目录把 `ECOHUB_IMAGE` 设为 `ghcr.io/fe-spark/ecohub:v3.1.2-beta.1`，然后执行 `docker compose pull && docker compose up -d`。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.1.2-beta.1 更新内容

- **片库规模**：移除增量计数缓存，改为采集结束后异步查库重算，前台展示「数据入库中」「计算中」状态。
- **采集停止**：支持探页即时取消与抓取命中取消直接丢弃，已停站点跳过同步发布锁以缩短停止等待。
- **跨源线路匹配（#110）**：跨源补全线路与海报时改为比对归一化一级大类语义，修复各站本地分类 ID 不同导致的误过滤。

---

测试版 **v3.1.2-beta.0**，镜像 `ghcr.io/fe-spark/ecohub:v3.1.2-beta.0`。带连字符的预发布标签不会把 `:latest` 指到此版本。正式版仍是下方的 v3.1.1。

中文 | [English](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE_EN.md)

### 升级说明

- 测试安装：在安装目录把 `ECOHUB_IMAGE` 设为 `ghcr.io/fe-spark/ecohub:v3.1.2-beta.0`，然后执行 `docker compose pull && docker compose up -d`。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.1.2-beta.0 更新内容

- **配置导入保留片库计数（#109）**：替换采集站时，本库已有同 ID 站点保留实时影片数，新站记为 0；导入拒绝重复采集站地址；备份里没有的模块不可勾选。
- **片库规模按全站汇总（#108、#109）**：工作台顶部影片、分类与当天失败页改为汇总全部采集站，与下方各站明细一致；主站影片数不再读过期缓存。
- **管理端交互**：顶栏主站选择器标明当前主站并修正对齐；管理密码确认弹窗支持回车提交（#108）。

---

正式版 **v3.1.1**，镜像 `ghcr.io/fe-spark/ecohub:v3.1.1`（`:latest` 同步指向此版本）。镜像 tag 说明见 [部署指南](https://github.com/fe-spark/EcoHub/blob/main/docs/README-Deploy.md)。

中文 | [English](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE_EN.md)

### 升级说明

- v3.0.x / v3.1.0：通过 `ecohub` 安装的，执行 `ecohub update`；使用 1Panel 或自行维护 compose 的，在安装目录执行 `docker compose pull && docker compose up -d`。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.1.1 更新内容

- **分类与轮播支持按采集站切换（#107）**：分类管理与轮播管理默认选中当前主站，支持切换管理任意采集站的分类排序、显隐及轮播排片。
- **隐藏分类不补充播放源**：非主站隐藏分类下的影片不再补充进主站影片与 TVBox 详情的播放源；保存分类或分类规则变更时同步清理播放详情缓存。
- **检索封口优化**：大批量采集封口复用已校验主站归属，达到阈值后改走整站顺序重建以降低内存峰值。
- **TMDB、素材与系统优化**：精简 TMDB 匹配流程并移除自动匹配后台任务；统一图片素材删除校验（先删磁盘再删记录）；重构代理、通知轮询、统一鉴权及管理端内容区布局。

---

正式版 **v3.1.0**，镜像 `ghcr.io/fe-spark/ecohub:v3.1.0`（`:latest` 同步指向此版本）。镜像 tag 说明见 [部署指南](https://github.com/fe-spark/EcoHub/blob/main/docs/README-Deploy.md)。

中文 | [English](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE_EN.md)

### 升级说明

- v3.0.x：通过 `ecohub` 安装的，执行 `ecohub update`；使用 1Panel 或自行维护 compose 的，在安装目录执行 `docker compose pull && docker compose up -d`。
- **第一次启动会建立排片评分索引** `idx_film_index_cat_score`。还没有影片列表索引 `idx_film_index_source_updated` 时，同一次启动也会建它，并按采集站重算片库影片数。片库很大时这些步骤会久一些。统计失败时原来的影片数保持不变。影片数据不改，不删采集记录。已运行过 `v3.1.0-beta.x` 的库可直接平滑升级。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.1.0 更新内容

- **全局主站与检索索引**：内存索引仅常驻当前主站，切主站立即切换并丢弃旧索引；完善全局主站切换、停用/删站交接校验、管理员权限隔离、批量删除排队撤回与删除期间顶栏状态提示，修复全选批量删除触发整页闪烁的问题（#98）。
- **分类规则与隐藏过滤（#106）**：一级分类规则合并出的新父组改用独立分类行与公开 ID；支持分类规则「全部分组」筛选、一级降级后二级二次合并，以及部分成员隐藏时保持合并组可见；修复首页推荐、大类三路榜单及轮播候选透出隐藏子分类的问题。
- **开放接口、TVBox 与工作台**：开放接口、分类列表与 TVBox 订阅仅提供当前主站并优化线路清洗排序；工作台展示当前主站及各站片库、分类与当天失败页统计。
- **搜索、筛选与排片**：支持非主站简繁/全半角匹配与字面量通配符转义；优化排片候选索引查询并过滤无播放线路影片。
- **数据重置与服务端加固**：优化百万级数据重置与初始化重建；统一加固弹幕限流与入参校验、SSRF 与解压体积上限防护、敏感配置脱敏及多处并发缓存安全。

---

正式版 **v3.0.6**，镜像 `ghcr.io/fe-spark/ecohub:v3.0.6`（`:latest` 同步指向此版本）。镜像 tag 说明见 [部署指南](https://github.com/fe-spark/EcoHub/blob/main/docs/README-Deploy.md)。

中文 | [English](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE_EN.md)

### 升级说明

- v3.0.x：通过 `ecohub` 安装的，执行 `ecohub update`；使用 1Panel 或自行维护 compose 的，在安装目录执行 `docker compose pull && docker compose up -d`。
- **从 v3.0.5 升级时，启动会清空采集记录**：删除 `collect_records`、`collect_record_films` 后按新结构重建。已经跑过 v3.0.6-beta.2 及之后测试版的库不会再清一次。影片库不在这两张表里，不受影响。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.0.6 更新内容

- **限流**：连续 20 页失败后取消在途请求，暂停约 2 分钟再继续未完成的页。已失败的页不重抓。暂停结束后重新计数，再失败 20 页才中断。
- **采集记录**：按时间、方式、范围、耗时、更新影片、页数、站点、状态分列，可自动刷新。进行中与采集中心一致。结束后保存本轮影片编号。
- **上次采集页**：开始采集时先归零，再跟着本轮成功页增加。结束后保留这次的页数。
- **探总页**：失败后再试 2 次，三次都失败才记为采集失败。

---

正式版 **v3.0.5**，镜像 `ghcr.io/fe-spark/ecohub:v3.0.5`（`:latest` 同步指向此版本）。镜像 tag 说明见 [部署指南](https://github.com/fe-spark/EcoHub/blob/main/docs/README-Deploy.md)。

中文 | [English](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE_EN.md)

### 升级说明

- v3.0.x：通过 `ecohub` 安装的，执行 `ecohub update`；使用 1Panel 或自行维护 compose 的，在安装目录执行 `docker compose pull && docker compose up -d`。
- **v3.0.3 不再读取 `HTTPS_PROXY`、`HTTP_PROXY`、`ALL_PROXY`**：编排文件和 `.env.example` 已移除这三个变量，旧 `.env` 中保留的值不再生效。此前依赖这些变量使弹幕或 Telegram 通过代理访问的，升级后须在后台「网络代理」开启全局代理，并开启「弹幕抓取」或「Telegram 机器人」模块，否则相关请求将直连。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.0.5 更新内容

- **播放页**：未指定片源时播放主站。可开启测速优选，对当前集各线路按持续速度自动选择；侧栏显示测速结果，支持重新测速，手动选择不会被自动结果覆盖。测速默认关闭。播放失败时只提示错误，不再自动切换线路（#87，#91）。
- **采集加入未封口的一轮**：封口周期已经打开、仍有采集队列在跑时，新的定时或手动采集加入这一轮，全部队列结束后再封口。当前没有队列时拒绝新开。同一批次里尚未开跑的站，不会因为前面的站已经进入待检索而被标成失败。自动采集重叠到点时，当前队列结束后再补采一轮当时空闲的站。不能新开时写明是检索索引正在更新，或这一轮检索即将入库，正在进行的采集会继续跑完。入队被拒绝时立刻放开已经占住的站（#92）。
- **检索索引封口**：全部采集队列结束后只封一次。仅当内存索引不可用、条目为 0 或读库失败时整表顺序加载，其余按变动编号增量打补丁。采集记录落库不挡住封口。只有本轮更新成功才结束采集（#92）。
- **写并发**：同时写库从 16 路满速起步，按最近几页耗时加减，最低 2，硬顶 16。耗时翻倍或遇到锁等待就减 1 路，变快再加回。同站仍然串行。页并发和取数跟着当前路数走。
- **临时断连**：已有成功页的站点连续遇到 EOF、连接重置或单页超时，先暂停约 15 秒再补抓这些页，不立刻记成部分失败。每页最多补两轮，整站最多短暂停 3 次；仍失败时再按连续失败页和 5 分钟长暂停处理（#92）。
- **工作台失败页**：工作台「采集失败页数」为北京时间当天的失败页合计，各站可以展开。批次明细里的错误可打开弹窗，分开显示原因、请求地址和失败页。新采集的错误原因保留到 2000 字（#95）。

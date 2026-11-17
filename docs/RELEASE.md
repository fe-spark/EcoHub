测试版 **v3.0.7-beta.6**，镜像 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.6`。带连字符的预发布标签不会把 `:latest` 指到此版本。正式版仍是下方的 v3.0.6。

### 升级说明

- 测试安装：在安装目录把 `ECOHUB_IMAGE` 设为 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.6`，然后执行 `docker compose pull && docker compose up -d`。`ecohub update` 只拉取 `.env` 里已有的镜像，不会自动改到这个测试标签。
- **第一次启动会建立排片评分索引** `idx_film_index_cat_score`。还没有影片列表索引 `idx_film_index_source_updated` 时，同一次启动也会建它，并按采集站重算片库影片数。片库很大时这些步骤会久一些。统计失败时原来的影片数保持不变。影片数据不改，不删采集记录。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.0.7-beta.6 更新内容

- **更新影片清单**：按片名搜索不再因为 LIKE 转义符写错而失败。搜「偏爱」不会再报 MySQL 1064。
- **影片列表**：切换到非主站后，无关键词分页走所属站更新索引取当前页，不再对整站排序把请求拖超时。

---

测试版 **v3.0.7-beta.5**，镜像 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.5`。带连字符的预发布标签不会把 `:latest` 指到此版本。正式版仍是下方的 v3.0.6。

### 升级说明

- 测试安装：在安装目录把 `ECOHUB_IMAGE` 设为 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.5`，然后执行 `docker compose pull && docker compose up -d`。`ecohub update` 只拉取 `.env` 里已有的镜像，不会自动改到这个测试标签。
- **第一次启动会建立排片评分索引** `idx_film_index_cat_score`。还没有影片列表索引 `idx_film_index_source_updated` 时，同一次启动也会建它，并按采集站重算片库影片数。片库很大时这些步骤会久一些。统计失败时原来的影片数保持不变。影片数据不改，不删采集记录。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.0.7-beta.5 更新内容

- **片名搜索**：带分类、地区、年份或最近更新的筛选，以及非主站剧情筛选，在 MySQL 上不再因为转义符写错而失败。
- **非主站搜索**：简体关键词能命中繁体片名，半角数字能命中全角数字。搜「庆余年2」可以找到「慶餘年２」。
- **采集封口**：入队前查所属站失败时，编号留在队列稍后重试，不再跳过这一轮。

---

测试版 **v3.0.7-beta.4**，镜像 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.4`。带连字符的预发布标签不会把 `:latest` 指到此版本。正式版仍是下方的 v3.0.6。

### 升级说明

- 测试安装：在安装目录把 `ECOHUB_IMAGE` 设为 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.4`，然后执行 `docker compose pull && docker compose up -d`。`ecohub update` 只拉取 `.env` 里已有的镜像，不会自动改到这个测试标签。
- **第一次启动会建立排片评分索引** `idx_film_index_cat_score`。还没有影片列表索引 `idx_film_index_source_updated` 时，同一次启动也会建它，并按采集站重算片库影片数。片库很大时这些步骤会久一些。统计失败时原来的影片数保持不变。影片数据不改，不删采集记录。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.0.7-beta.4 更新内容

- **排片**：每个分类按对应索引只取前几部再合并，不再对整站排序。没有播放线路的影片在取前几部时就被跳过。
- **片名搜索**：非主站搜索按更新时间每批 2000 条读取再打分。宽关键词不再一条语句读完整站。
- **采集封口**：所属站查询失败时，编号留在队列，约 2 秒后再封。
- **影片列表**：采集站上记的影片数还是 0 时会再数一遍，不会把这一站显示成空的。
- 镜像里同时包含尚未单独发布的 beta.3：非主站列表按所属站和更新时间取页，中文搜索先按字筛选再打分，切换主站失败时按当前主站补建检索索引。

---

测试版 **v3.0.7-beta.3**，镜像 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.3`。带连字符的预发布标签不会把 `:latest` 指到此版本。正式版仍是下方的 v3.0.6。

### 升级说明

- 测试安装：在安装目录把 `ECOHUB_IMAGE` 设为 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.3`，然后执行 `docker compose pull && docker compose up -d`。`ecohub update` 只拉取 `.env` 里已有的镜像，不会自动改到这个测试标签。
- **第一次启动会建立影片列表索引** `idx_film_index_source_updated`，并按采集站重算片库影片数。片库很大时这两步会久一些。统计失败时原来的影片数保持不变。影片数据不改，不删采集记录。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.0.7-beta.3 更新内容

- **影片列表**：非主站按所属站和更新时间取一页，不再用播放线路编号回表。本地约 7 万到 17 万部的站，第一页在几十毫秒。
- **片名搜索**：非主站中文搜索先按搜索词里的每个字筛选，再打分，只保留得分最高的 200 条。拼音搜索仍读取这一站的片名。
- **切换主站**：预热中途发现主站已变，或重建失败时，按当前主站补建检索索引。

---

### v3.0.7-beta.2

测试版 **v3.0.7-beta.2**，镜像 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.2`。

### 升级说明

- 测试安装：在安装目录把 `ECOHUB_IMAGE` 设为 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.2`，然后执行 `docker compose pull && docker compose up -d`。`ecohub update` 只拉取 `.env` 里已有的镜像，不会自动改到这个测试标签。
- **第一次启动会按采集站重算片库影片数**。片库很大时这一步会久一些。影片数据不改，不删采集记录。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.0.7-beta.2 更新内容

- **启动**：不再给 `film_index` 建 `(first_source_id, deleted_at)` 复合索引。beta.1 在百万级片库上建这个索引会超过 MySQL 30 秒读超时，进程退出；索引实际已建成时，下一次启动又会因重名失败。影片数仍记在采集站上。

---

### v3.0.7-beta.1

测试版 **v3.0.7-beta.1**，镜像 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.1`。

### v3.0.7-beta.1 更新内容

### 升级说明

- 测试安装：在安装目录把 `ECOHUB_IMAGE` 设为 `ghcr.io/fe-spark/ecohub:v3.0.7-beta.1`，然后执行 `docker compose pull && docker compose up -d`。`ecohub update` 只拉取 `.env` 里已有的镜像，不会自动改到这个测试标签。
- **第一次启动会按采集站重算片库影片数**。片库很大时这一步会久一些。影片数据不改，不删采集记录。
- v2.x：不兼容，不提供迁移。见 [从 v2 升级到 v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3.md)。

### v3.0.7-beta.1 更新内容

- **检索索引**：内存里只常驻当前主站。切换主站后立刻丢掉上一站索引，重建完成前不能再切主站或改站点。所属站查询失败时放弃这一次封口，不把未过滤编号写进索引，也不因此整表重建（#98）。
- **工作台**：顶部是当前主站的片库影片、分类、当天失败页。展开后看各采集站的这三项。影片数记在采集站上，展开不再扫描影片表。切换主站后等排序完成再刷新，不必手动刷新页面。
- **开放接口与 TVBox**：分类、列表和订阅只提供当前主站。订阅名称用网站设置。播放详情仍带上其他站的线路。

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

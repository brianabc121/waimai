# 海外餐饮外卖系统 · Claude 开发交付包

版本 v1.0 · 2026-11-21

这是供 Claude 在开发工作区使用的项目资料包。解压到项目目录，在 Claude 中打开该目录。已有仓库可先保留现有项目说明，把本包放在独立子目录，给 Claude 指明本包的 `CLAUDE.md`。

可以直接发送这句：

> 请读本资料包的 CLAUDE.md 和 IMPLEMENTATION_TASKS.md，按 Go + Gin + PostgreSQL + Docker Compose 开发。先落实阶段 0 和阶段 1，实际启动本地服务并验证扫码／iPad 下单、工作台分单、加菜和关台，再汇报交付、测试结果以及下一阶段。

| 文件 | 用途 |
|---|---|
| [CLAUDE.md](CLAUDE.md) | 开发入口，约定首版范围、资料优先级和交付要求。 |
| [IMPLEMENTATION_TASKS.md](IMPLEMENTATION_TASKS.md) | 分阶段任务、首个可演示闭环和验收场景。 |
| [Gin 开发设计](docs/Restaurant_Gin_Development_Design_v1.0.md) | 后端架构、数据模型、事务、API、权限、实时与部署。 |
| [功能设计总稿](docs/Restaurant_Functional_Design_v1.0.md) | 完整需求、33 个页面、角色和跨页业务规则。 |
| [33 份逐页说明](docs/page-design/README.md) | 按页面 ID 查找对应实现与验收说明。 |
| [UI 效果图索引](ui/README.md) | 中文顾客菜单、扫码、iPad、商家后台、后厨和骑手效果图。PNG 已随包提供。 |
| [早期技术选型](docs/reference/Restaurant_Technology_Selection_v1.0.md) | 前端、门店设备和第三方接入的历史评估。首版后端与部署以 Gin 开发设计为准。 |

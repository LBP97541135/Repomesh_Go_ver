# 原型整体串联 · 待评审 r1

2026-09-12归档补充：当前保留11份原稿，4份明确被替代稿移至[历史归档](../archive/2026-09-12-development-preparation/README.md)；“全貌与原稿”仍列全部15份来源。下文历史检查中的15份计数保留当时含义，当前生成清单只覆盖本目录11份原稿。2026-09-20补充：新增独立单页[计划执行进度板](../prototypes/dag-plan-progress.html)（DAG 计划执行进度，2026-09-16 稿），不进串联预览也不进生成清单，本目录原稿共12份、生成清单仍为11份。

用户本轮要求：“你阅读并先把D:\Project4work\Repomesh\_Go\_ver\docs\prototypes整个串联起来，再来讨论。”目录实际路径沿当前工作区 `D:\Project4work\Repomesh_Go_ver\docs\prototypes`。本稿和原型只准备可讨论的整体，不表示用户已经采用新的导航或跨页行为。逐项用户同意及最多三轮规则继续有效。

入口：[完整串联原型](../prototypes/index.html)。原型导航条中的“全貌与原稿”列出全部15份原始HTML、采用范围、替代关系及独立查看链接。主流程保留当前采用片段；旧稿不删除，也不作为新的正常入口。

后端设计师2已对本r1说明的契约／采用／实现边界ACK，RM-PROTOTYPE-LINK为1／3，无分歧。该ACK不包含生成器、原稿哈希或浏览器行为的独立验收，用户尚未采用整体串联。

## 阅读所得与片段来源

本次遍历目录全部15份HTML及README，读取页面结构、状态初始化、事件入口和后继脚本差异；重复源码按同源片段对照。重点展开当前四组完整状态与交互代码，原始视觉样式直接复用。不是对全部CSS逐行审计，也不是业务实现检查。

| 原型文件 | 串联用途 | 状态与替代关系 |
| --- | --- | --- |
| `repomesh-project-entry-prototype.html` | 登录、选仓、资料与保存后入口 | 已采用两步顺序和授权提示；真实认证未设计完整。 |
| `repomesh-project-settings-prototype.html` | 同一项目资料、增仓、配置引用与原更新恢复 | 已采用三段顺序、显式配置开关、摘要后确认；要求主界面可找到，完整F02未完。 |
| `repomesh-model-provider-prototype.html` | 供应商、模型编辑、测试与应用演示 | r3分栏及参数填写已采用；应用、Key、测试完整协议未完成。 |
| `repomesh-issue-modal-prototype.html` | Issue列表打开居中弹窗的源基线 | 大小、字段排布已采用。 |
| `repomesh-issue-conversation-prototype.html` | 创建弹窗关联新建／已有会话 | 位置和展开已采用，后继主工作区包含该片段。 |
| `repomesh-issue-submit-prototype.html` | 创建中、未知、查询及成功入口 | 展示文案／按钮已采用；完整恢复缺口不变。 |
| `repomesh-issue-detail-prototype.html` | 概览、业务会话、房间、历史及交付 | 概览／验证交付待评审。 |
| `repomesh-issue-dag-prototype.html` | 详情页签、右侧面板的只读图和节点详情 | 图布局及图下详情已采用；真实图协议未完成。 |
| `repomesh-conversation-dock-prototype.html` | 悬浮入口，同页右侧详情／DAG／Leader | r2已采用；旧固定索引不回流。 |
| `repomesh-message-target-prototype.html` | 主工作区整合底稿，包含上述后继片段；澄清作为显式脚本场景 | 控件未采用；默认普通会话，不默认激活待审澄清。 |
| `repomesh-conversations-issues.html` | 会话与独立Issue的整体视觉来源 | 具体交互以其后继稿为准。 |
| `repomesh-conversation-navigation-prototype.html` | 历史查看 | 固定索引和离页方案被dock r2替代。 |
| `repomesh-model-settings-prototype.html` | 历史查看 | 连接列表／整体编辑弹窗被provider r3替代。 |
| `repomesh-project-first.html` | 历史查看 | 会话与Issue拆分前版本。 |
| `repomesh-rooms-prototype.html` | 历史查看 | 旧草稿绑定已被替代。 |

## 待讨论的整体路径

1. 登录演示 → 当前已发现仓库 → 项目资料 → 保存摘要 → 进入该项目工作区。项目保存不会自动产生会话／Issue或设置Ready。
2. 工作区右上角“⚙ 项目设置”（侧栏折叠仍显示；原侧栏入口保留）→ 项目设置 → “← 返回工作区”。用户已同意F02三项页面安排并要求主界面入口，具体位置在本次原型展示；模型设置及其他跨页衔接仍待审。项目名、用途和明确增仓在本次预览的同一项目中衔接；在途Issue范围保留。
3. Issue列表 → 居中创建 → 新建／已有会话 → 提交等待／未知查询 → 详情／关联会话。
4. 会话 → 常驻悬浮入口 → 同页右栏详情／只读DAG／Leader → 关闭面板，主会话保留。
5. Issue详情 → 图／规格、验证与交付、历史 → 返回列表／关联会话。
6. “直接查看场景”可进入双Issue会话、澄清、DAG、交付和新建项目；“全貌与原稿”可对照所有15份来源。

顶部流程条、“全貌与原稿”和直接场景选择器是评审工具；侧栏新增设置链接、保存后进入工作区以及返回行为为本次串联草案，不冻结产品导航。所有新的设计选择仍待用户明确同意。

当前模型“应用”已由F04独立样例预览替代原简化摘要写入；演示结果与版本仅保存在F04片段，不写F02项目配置。它没有修改现行configuration PATCH或生成真实profile／SecretVersion；具体替代及未完边界见文末F04。旧完整HTML及原有采用范围保持。

## 原型文件与运行边界

- 页面独占：本稿、`docs/prototypes/index.html`、`assemble-prototypes.py`和`joined-preview-shell.html.template`。本目录保留12份原始HTML（其中[计划执行进度板](../prototypes/dag-plan-progress.html)为独立单页、不进串联），另4份历史稿已归档；生成入口保存当前11份串联原稿SHA-256，历史原始字节与映射见归档清单。
- 生成：仓库根目录运行 `python docs/prototypes/assemble-prototypes.py`。四组当前页面嵌入一个HTML，额外适配仅用于原型导航和内存样例衔接。
- 预览：沿用本地只读静态服务 `http://127.0.0.1:8769/index.html`。不运行产品或真实外部服务。
- 页面草稿和原型未知状态留在内存页面实例中；F02按项目分开保留。浏览器刷新重置全部样例，URL不是业务恢复契约；原稿新标签也不共享本次内存。
- F06执行参数编辑、F11多Issue消息／更正、F15分析面板等完整页面仍不存在；不添加可假执行的按钮。F01—F15保持未完成，各项现有采用范围不变。

检查与用户反馈补记在[本轮页面日志](../archive/2026-09-12-development-preparation/docs/current/design-communication-page-2026-09-11.md)。技术对齐不替代用户采用；原型走查不代表真实权限、事务、模型应用、消息投递或执行验证。

## F03追加展示

用户“继续下一项”后增加[仓库选择器待审r1](repository-picker-design.md)，由repository-picker.prototype.js嵌入F02添加区，生成器读入；原15份HTML保留。新增预览路由index.html#repository-picker只定位讨论场景，不是产品路由。选择摘要、搜索／加载更多与授权异常只为内存演示，未新增采用或真实发现能力。

## F04追加展示

F03三项页面安排已获用户“同意，继续下一项”采用，完整发现与恢复仍未完。新[模型用于项目F04](model-project-apply-design.md)由model-project-apply.prototype.js嵌入已采用供应商／模型原型；预览、条件变化、未知／原结果核查及回读均为独立样例状态。该片段不再沿旧简化应用按钮写入串联演示模型摘要，以免把其样例execution／密钥版本当成当前项目事实；F02未知值保持。F04三项UI尚待用户同意。

## 首批范围采用后：最小Issue概览

用户确认首批范围并明确只做页面与API设计、开发以后单独做。新增[最小概览r2](issue-overview-minimal-design.md)，生成器将issue-overview.prototype.js嵌入工作区；只精简独立概览，不覆盖已采用的DAG／dock原稿。显式高级场景仍供历史对照，当前详情三项UI待用户采用。

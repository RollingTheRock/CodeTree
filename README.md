# codetree (`ct`)

终端里的代码结构地图，配合终端编辑器（如 Helix）使用。形态参考 charm 的 glow：

- **裸跑 `ct`**（stdout 是 TTY 且无参数）→ 打开 TUI 符号浏览器
- **带参数 / 管道** → 纯 CLI，输出文本树到 stdout，脚本友好

设计公理：CLI 优先、即开即走、查询与渲染分离（core 产出结构化符号图，渲染与 TUI 都是薄壳）。

## 安装

```sh
go install codetree/cmd/ct@latest   # 或克隆后: go build -o ct ./cmd/ct
```

> Python 支持基于 tree-sitter（cgo），构建需要 `CGO_ENABLED=1` 和 C 编译器。

## CLI 用法

```
ct [flags] [path]        # path 默认当前目录，也可以是单个文件

  -a, --all              显示全部符号（含变量/常量，默认过滤降噪）
  -d, --depth N          限制树深度：1=只到文件，2=到类/顶层符号，3=到方法
  -f, --format FORMAT    text | json | mermaid（默认 text；非 TTY 强制 text）
  -l, --lang LANG        强制语言（python|go），默认按扩展名自动混合扫描
```

扫描默认尊重根目录 `.gitignore`，并跳过 `vendor/`、`node_modules/`、`.git/`、`__pycache__/` 等常见目录。

示例输出：

```
myproject/
├── models/
│   ├── animal.py
│   │   ├── Animal (class)
│   │   │   └── speak(self)
│   │   └── Dog(Animal) (class)
│   │       ├── speak(self)
│   │       └── fetch(self)
│   └── zoo.py
│       └── make_sound(a) (func)
```

- `ct -f json .` —— 结构化 JSON（含 kind、file:line、docstring、supertypes、fields），供脚本消费
- `ct -f mermaid .` —— 合法 Mermaid `classDiagram`，含继承边 `Animal <|-- Dog`
- `ct -f diagram .` —— 终端内字符画布 UML 类继承图（自研 tidy-tree 布局，无第三方布局库）
- `ct -lsp -f diagram .` —— 先跑 LSP 语义修正（按项目语言逐个起 server），stderr 打印修正 diff，再渲染修正后的模型

### 类图（-f diagram）

```
ct -f diagram [path]            # 全局类继承图（实线三角 ▲ = 继承，绿色虚线 = implements）
  --members                     # 展开字段/方法舱室（默认收起，只显示类名）
  --focus <Class>               # 邻域模式：只显示焦点类的祖先链 + 后代
  --up N                        # 祖先层数（默认 -1 = 全部）
  --down N                      # 后代层数（默认 2）
  --siblings                    # 邻域模式中同时显示兄弟类
  --external=false              # 隐藏项目外的基类盒子（默认灰色显示）
  --assoc=false                 # 关闭组合边（默认开：字段类型引用项目内类 → ◆ 琥珀色实线）
```

组合/聚合（composition）：字段类型经剥壳（`list[Insect]`→`Insect`、`dict[str, Foo]`→`Foo`、
`Optional[Foo]`/`Foo | None`→`Foo`、`Foo[]`/`[]Foo`/`*Foo`→`Foo`、`map[string]Foo`→`Foo` 等）后匹配
项目内类，画持有方 `◆` 菱形 + 实线（无箭头，UML 语义）；内置/标量/小写基础名不进图，同一对
owner→target 去重、自引用跳过。有组合关系的类自动从 unrelated 分区提升到主图。

**文件作用域**：位置参数指向文件（可多个）时，自动向上探测项目根（`.git`/`pyproject.toml`/`go.mod` 等 marker，找不到用父目录），扫描全项目但只把**这些文件里定义的类当主角**；它们的完整祖先链 + 直接子类作为上下文节点拉入图中，弱化显示并标注来源文件（`C Module ·models/base.py`）：

```
ct -f diagram models/networks.py              # 单文件作用域
ct -f diagram models/networks.py models/blocks.py   # 多文件并集
```

多重继承：第一个基类参与布局（实线），其余基类在卡片标题上标注 `+Base`。
无血缘关系的孤儿类在全项目图下方的「unrelated classes」分区单独显示（文件作用域/邻域模式下不显示）。
图超过终端宽度时会在 stderr 提示并建议使用 `--focus`；管道输出自动去 ANSI。

## TUI（picker-first 流程）

裸跑 `ct` 首先进入**文件选择器**（Helix `Space f` 风格），确认后进入类图。

文件选择器（列表模式）：

| 键 | 动作 |
|---|---|
| `/` | 进入过滤模式（模糊过滤文件列表） |
| `j`/`k` / `↑`/`↓` | 移动光标 |
| `Space` | 标记/取消标记（多选，可跨过滤结果累积） |
| `Enter` | 确认：有标记按标记集合、无标记取光标文件 → 进入类图 |
| `t` | 切到文件树视图 |
| `?` | 帮助浮层（任意键关闭） |
| `Esc` / `q` | 退出 |

过滤模式（fzf 式）：

| 键 | 动作 |
|---|---|
| 输入字符（含空格） | 编辑过滤词，列表实时过滤 |
| `↑`/`↓`（或 `Ctrl+n`/`Ctrl+p`） | 在过滤结果中移动 |
| `Tab` | 标记/取消标记当前行并下移（`Shift+Tab` 上移） |
| `Enter` | 确认进入类图 |
| `Esc` | 退出过滤模式，保留过滤词 |

类图视图：

| 键 | 动作 |
|---|---|
| `h`/`j`/`k`/`l` / 方向键 | 结构化移动选中类（高亮）：j=最左子类，k=父类，h/l=同父兄弟（按视觉 X 序），兄弟尽头跳到相邻树的根 |

导航只认布局主骨架（继承树父子关系）；implements 虚线边和组合 ◆ 边不产生导航关系。邻域模式下规则作用于过滤后的子图。
| `Enter` | 进入邻域模式（焦点 = 选中类） |
| `Esc` | 先退出邻域模式；再按返回选择器（标记保留） |
| `A` | 切全项目作用域 |
| `+` / `-` | 调整邻域向下层数 |
| `b` / `m` | 兄弟类显示 / 舱室展开收起 |
| `c` | 组合边（◆）开关 |
| `t` | 切到文件树视图 |
| `?` | 帮助浮层 |
| `o` / `q` | 跳编辑器 / 退出 |

文件树视图（保留为可选视图）：

| 键 | 动作 |
|---|---|
| `j`/`k` / `↑`/`↓` | 移动光标 |
| `h` / `←` / `l` / `→` / `Enter` / `Space` | 折叠 / 展开 / 切换 |
| `/` | 过滤（`Esc`/`Enter` 退出） |
| `t` | 返回选择器 |
| `?` | 帮助浮层 |
| `o` / `q` | 跳编辑器 / 退出 |

画布超过视口时自动平移跟随选中项，状态栏右侧显示滚动位置（`▲ 12/48 ▼`）。

**动态刷新**：TUI 运行期间递归监听项目目录（fsnotify，遵守 .gitignore 和默认跳过目录），
保存文件后 picker/树/类图自动重建，300ms 防抖合并编辑器的写入脉冲；标记集合、作用域、
focus、选中类和滚动位置都会恢复，状态栏显示 `↻ HH:MM:SS` 刷新时间。监听启动失败时静默降级为不刷新。
底栏为 airline 风格状态栏：左侧模式徽章（PICKER 绿 / DIAGRAM 青 / FOCUS 黄+焦点类名 / TREE 紫），
中间键位提示（键名亮色加粗、描述暗色），右侧滚动位置；窄终端（<80 列）自动降为只显示键名。

右侧（窄窗口为下半区）显示当前符号详情：文件：行号、签名、基类、Python docstring 首段。

## 架构

```
cmd/ct/main.go     入口（标准库 flag；TTY 分流 CLI/TUI）
core/              符号模型(Symbol/Kind/File/Project) + 项目扫描 + gitignore ── 纯逻辑，不碰 UI
langs/             Language 接口 + 注册表（init() 自注册，可插拔）
langs/python/      tree-sitter queries（smacker/go-tree-sitter, cgo）
langs/java/        tree-sitter-java：extends/implements 分离（Implements 字段）
langs/cpp/         tree-sitter-cpp：class/struct/enum、模板基类、类外定义归并
langs/golang/      标准库 go/ast 实现 —— 证明可插拔且不增加 cgo 负担
render/text/       缩进树（│ ├─ └─ 引导线）
render/json/       结构化 JSON
render/mermaid/    Mermaid classDiagram（含继承边）
lsp/               可选 LSP 语义层（多语言）：go.lsp.dev/protocol 客户端 +
                   server 探测（TOML 配置，可插拔）+ 事实修正/增补（resolver）
diagram/           字符画布 UML 类图：建继承森林 → Buchheim tidy-tree 布局（字符格）
                   → 方向位掩码 elbow 布线 → 卡片渲染；纯函数，不依赖 TUI
tui/               bubbletea 浏览器（薄壳，只消费 core.Project / diagram.Diagram）
```

数据流：`langs/*` 把源码解析成 `core.Symbol` 树 → `core.Scan` 聚合成 `core.Project` →
`render/*` / `tui/` 各自消费。查询与渲染完全分离。

符号模型带 LSP 语义层扩展位：`Symbol.BasePos`（基类 token 位置）、`Symbol.BaseRefs`（LSP 解析后的精确绑定）、`Symbol.Source`（static/lsp 来源标记）、`Field.Line/Col`（hover 定位用）。

## 当前支持

- **Python**：class（含文本级基类列表）、模块级函数、类内方法、嵌套类/嵌套函数、`async def` 标注、装饰器摘要（`@property` 等）、docstring 首段、变量/常量（`-a`）、类属性 + `__init__` 内 `self.x` 实例属性（类型从注解或值推断）；`Protocol/ABC` → interface、`Enum` → enum
- **Go**：`type struct` / `type interface`、函数、方法（按接收者归到对应 type 下；跨文件接收者以 `(T)` 标注）、常量/变量（`-a`）、doc 注释首段、struct 字段（嵌入字段进 SuperTypes 出继承边；LSP 下 interface 实现关系出虚线边）
- **Java**：class / interface（含 `extends` 链）/ enum / record、构造器（`new` 标注）、`@Override` 等注解摘要、字段（含类型）、嵌套类归层；**extends → 实线继承边，implements → 绿色虚线边**
- **C++**（.cc/.cpp/.cxx/.h/.hpp/.hxx）：class / struct / enum（含 `enum class`）、`base_class_clause` 多基类（忽略访问修饰符，模板基类 `B<T>` 取 `B`）、构造/析构、类内方法声明+定义、namespace 内类型、模板类（名字不带模板参数）、类外定义 `void Dog::bark()` 按限定名归并
- **Rust**（.rs）：struct / enum / trait（→ interface）、impl 块方法归并到类型、`impl Trait for Type` → Implements 虚线边、字段（含类型）
- **TypeScript/JavaScript**（.ts/.tsx/.js/.jsx）：class（extends → 继承边；TS implements → 虚线边）、interface / enum、方法、TS 字段（含类型）；JS 侧无字段（grammar 限制）

类图边语义：实线 `▲` = 继承（extends），绿色虚线 `┆/┄` = 实现（implements），灰色 = 外部基类。无法解析的 implements 接口降级为卡片标题 `~Iface` 标注。

## LSP 语义层（可选）

静态扫描打底、LSP 修正与增补：TUI 永远先用静态结果秒开；PATH 里探测到 server 就异步热身，就绪后自动刷新视图；没装 server 就安静保持纯静态，零报错零卡顿。状态栏右侧显示 `lsp warming… / ready / failed`。

默认 server 探测表：python → basedpyright/pyright；go → gopls；cpp → clangd；java → jdtls；rust → rust-analyzer；typescript/javascript → typescript-language-server（`--stdio`，需要 workspace 里有 typescript@5 依赖）。

修正四类事实：

- **基类消歧**：`definition` 打在基类 token 上，extends 边绑定到精确符号——两个文件各有同名 `Base` 时不再接错边（同名类现在都作为独立节点出现）
- **字段类型**：`hover` 补全静态推断不出的字段类型（如 `self.x = Dog()` 无注解场景），组合 ◆ 边更全
- **类增补**：`documentSymbol` 捞回静态漏掉的动态类（`Color = Enum('Color', ...)`、`namedtuple` 等），带 `lsp` 来源标记
- **接口实现（Go）**：`implementation` 打在 interface 上，实现者写回 `Implements`——Go 项目因此出现虚线 implements 边

server 可插拔、不捆绑，配置文件 `~/.config/codetree/config.toml`：

```toml
[lsp.python]
command = "basedpyright-langserver"   # 或 pyright-langserver / jedi-language-server
args = ["--stdio"]
# enabled = false                     # 关掉这一层
```

配置段按语言分 key：`[lsp.python]`、`[lsp.go]`、`[lsp.cpp]`、`[lsp.java]`、`[lsp.rust]`、`[lsp.typescript]`、`[lsp.javascript]`。CLI 侧用 `ct -lsp` 跑修正并打印 diff。

## 路线图

- type hierarchy / call hierarchy 更深的关系数据
- java 的 jdtls 路径本轮只有 fake-server 测试，未做真实 e2e

## 开发

```sh
CGO_ENABLED=1 go build ./...
CGO_ENABLED=1 go test ./...        # golden 更新: go test ./render/... -update
CGO_ENABLED=1 go vet ./...
```

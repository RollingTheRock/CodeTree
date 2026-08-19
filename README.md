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

- `ct -f json .` —— 结构化 JSON（含 kind、file:line、docstring、supertypes），供脚本消费
- `ct -f mermaid .` —— 合法 Mermaid `classDiagram`，含继承边 `Animal <|-- Dog`

## TUI 键位

| 键 | 动作 |
|---|---|
| `j`/`k` / `↑`/`↓` | 移动光标 |
| `h` / `←` | 折叠（已折叠则跳到父节点） |
| `l` / `→` / `Enter` | 展开 |
| `Space` | 折叠/展开切换 |
| `/` | 进入过滤模式（按名字模糊过滤，`Esc`/`Enter` 退出） |
| `o` | 用 `$EDITOR`（fallback `hx` → `vim`）打开文件并跳到符号行 |
| `q` / `Ctrl+C` | 退出 |

右侧（窄窗口为下半区）显示当前符号详情：文件：行号、签名、基类、Python docstring 首段。

## 架构

```
cmd/ct/main.go     入口（标准库 flag；TTY 分流 CLI/TUI）
core/              符号模型(Symbol/Kind/File/Project) + 项目扫描 + gitignore ── 纯逻辑，不碰 UI
langs/             Language 接口 + 注册表（init() 自注册，可插拔）
langs/python/      tree-sitter queries（smacker/go-tree-sitter + tree-sitter-python, cgo）
langs/golang/      标准库 go/ast 实现 —— 证明可插拔且不增加 cgo 负担
render/text/       缩进树（│ ├─ └─ 引导线）
render/json/       结构化 JSON
render/mermaid/    Mermaid classDiagram（含继承边）
tui/               bubbletea 浏览器（薄壳，只消费 core.Project）
```

数据流：`langs/*` 把源码解析成 `core.Symbol` 树 → `core.Scan` 聚合成 `core.Project` →
`render/*` / `tui/` 各自消费。查询与渲染完全分离。

符号模型为 v2 预留了 LSP 扩展位：`Symbol.SuperTypes`（v1 为文本级基类名，v2 由 LSP 填精确类型）、`Symbol.Doc`。

## 当前支持

- **Python**：class（含文本级基类列表）、模块级函数、类内方法、嵌套类/嵌套函数、`async def` 标注、装饰器摘要（`@property` 等）、docstring 首段、变量/常量（`-a`）
- **Go**：`type struct` / `type interface`、函数、方法（按接收者归到对应 type 下；跨文件接收者以 `(T)` 标注）、常量/变量（`-a`）、doc 注释首段

## v2 路线图（LSP 语义层）

- LSP 语义层：以 v1 静态树为骨架，用 gopls / pyright 等填精确符号信息
- type hierarchy / call hierarchy（`SuperTypes` 升级为解析后的精确类型）
- 后台 daemon：文件监听 + 增量索引，TUI 实时刷新
- 更多语言（rust/typescript），复用 tree-sitter 通道

## 开发

```sh
CGO_ENABLED=1 go build ./...
CGO_ENABLED=1 go test ./...        # golden 更新: go test ./render/... -update
CGO_ENABLED=1 go vet ./...
```

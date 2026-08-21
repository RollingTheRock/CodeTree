# codetree (`ct`)

终端里的代码结构地图。扫一眼项目，把类之间的继承、实现、组合关系画成一张能用 hjkl 走来走去的 UML 类图——写代码之前先看清楚几十上百个类的关系，不用开重型 IDE。

[English README](README.md)

<p align="center"><img src="demo.gif" alt="codetree demo" width="800"/></p>

## 功能

- 类图渲染在终端字符画布上：继承（实线）、接口实现（虚线）、组合（◆）
- 默认看单个/几个文件的作用域，`A` 切全项目
- `enter` 邻域聚焦一个类，`+`/`-` 调深度，`b` 带兄弟
- `m` 展开成员舱室（字段带类型、方法带签名）
- 文件保存自动刷新；`o` 跳进 `$EDITOR` 对应行
- 装了 LSP server 就异步把图修得更准（同名基类消歧、Python 无注解字段类型、Go 接口实现），没装就纯静态，一样快
- Python / Go / Java / C++ / Rust / TypeScript / JavaScript

## 安装

```sh
go install github.com/RollingTheRock/CodeTree/cmd/ct@latest
```

或从 [Releases](https://github.com/RollingTheRock/CodeTree/releases) 下载单二进制（Linux / macOS）。

## 用法

```sh
ct                  # TUI：空格选文件，回车看类图
ct -f diagram .     # 直接把类图打印到终端
ct -f mermaid .     # 输出 Mermaid classDiagram
ct -f json .        # 结构化 JSON，给脚本用
ct -lsp -f diagram . # 先跑 LSP 修正再出图，stderr 打印修正了哪些
```

按键：`hjkl` 移动 · `space` 标记文件 · `enter` 聚焦 · `esc` 返回 · `m` 成员 · `c` 组合边 · `/` 过滤 · `?` 帮助 · `q` 退出

### 邻域聚焦

<p align="center"><img src="docs/focus.gif" alt="focus mode" width="800"/></p>

### CLI 输出与 LSP 修正 diff

<p align="center"><img src="docs/cli.gif" alt="cli and lsp diff" width="800"/></p>

## LSP（可选）

不捆绑任何 server。PATH 里（或 `~/.local/share/jdtls`、nvim mason 目录）找到对应语言的 server 就自动用：pyright/basedpyright、gopls、clangd、jdtls、rust-analyzer、typescript-language-server。想换或想关，编辑 `~/.config/codetree/config.toml`：

```toml
[lsp.python]
command = "basedpyright-langserver"
args = ["--stdio"]
# enabled = false
```

## 开发

```sh
CGO_ENABLED=1 go build ./...   # tree-sitter 需要 cgo
go test ./...
```

GIF 用 [vhs](https://github.com/charmbracelet/vhs) 录制（`vhs demo.tape`）。

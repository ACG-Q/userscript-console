# tests/fixtures —— 外部站点 HTML/JSON 快照（D-06）

适配器（`internal/sources`）的回放测试目前使用**内联 canned 字符串**。
在对接真实站点前，须先抓取真实快照落入本目录（每个源 2 份），再核对适配器假设：

- `greasyfork/` —— 页面 HTML 三级回退链（内嵌头块 / 安装直链 `update.<域>.org` / `/code` 源码页；locale 前缀、title 分隔符假设）。一级直链 `https://update.greasyfork.org/scripts/<id>.user.js` 已对真实站点实测（旧假设 `<id>.code.user.js` 形态线上 404，已废弃）
- `userscript.zone/` —— 页面 HTML（是否服务端渲染头块）
- `gist/` —— `api.github.com/gists/<id>` 响应 JSON（truncated 大文件场景）
- `direct/` —— 直链 .user.js

命名：`<host>_<场景>.html|json`。抓取方式：浏览器另存或 `curl -A "<浏览器 UA>"`。
fixture 入仓后，适配器测试改为「文件回放」而非内联字符串（SPEC-ARCH-TEST §3.1）。

# userscript-console

GitHub 无服务器油猴脚本控制台：用 issue 评论驱动脚本注册表（registry.json），`usm` CLI 负责编译与发布 GitHub Pages 站点。本仓是**工具与文档**所在地；脚本数据（registry.json、源码、dist）由各使用方仓库持有。

## 命令文档

本页目录与全部命令页（面板 `/add` `/list` `/info` `/rm` `/sync` `/project` `/build` `/cleanup` 与 CLI 汇总）由 `usm build` 从 `docs/`、`docs/commands/` 自动生成，无需手工维护导航。

> 命令文档页间的相对链接在 GitHub 网页浏览源码时失效——已知取舍，以发布站点为准。

## 开发者文档

- [扩展脚本源适配器](https://github.com/ACG-Q/userscript-console/blob/master/docs/dev/extending-sources.md)（仓库内 `docs/dev/`，不转换为站点页）

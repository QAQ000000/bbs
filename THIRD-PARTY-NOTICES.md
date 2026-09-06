# 第三方组件与许可声明

本项目在 AGPL-3.0-or-later 下分发（见 LICENSE）。项目自身不含任何第三方论坛程序的代码或美术素材；
运行时目录（如 data/uploads、data/smiley）中的内容由站点运营者自行上传，其版权与合规责任由运营者承担。

## Go 依赖

| 组件 | 用途 | 许可证 |
| --- | --- | --- |
| [github.com/jackc/pgx/v5](https://github.com/jackc/pgx) | PostgreSQL 驱动与连接池 | MIT |
| [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) | bcrypt 密码哈希 | BSD-3-Clause |
| [golang.org/x/text](https://pkg.go.dev/golang.org/x/text) | Unicode 文本处理（pgx 间接依赖） | MIT |

## 内置表情

默认表情为 Unicode Emoji 字符（渲染为字符本身），短代码命名沿用社区通用惯例；
Unicode 字符内容为标准文本，不构成任何第三方的图形作品。

站点管理员可通过 `forumd -import-smileys` 导入自有素材，素材的版权与授权由导入者自行确认。

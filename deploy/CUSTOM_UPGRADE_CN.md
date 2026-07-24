# Sub2API Custom 公开版本升级

公开仓库的部署、在线更新、runtime 持久化和回退说明见：

- [`../docs/custom/OPERATIONS_CN.md`](../docs/custom/OPERATIONS_CN.md)
- [`../docs/custom/REPOSITORY_RELEASE_CN.md`](../docs/custom/REPOSITORY_RELEASE_CN.md)
- [`../docs/custom/SECURITY_CN.md`](../docs/custom/SECURITY_CN.md)
- [`rehearsal/README_CN.md`](rehearsal/README_CN.md)

部署必须固定 `ghcr.io/o87110/sub2api-custom-public:vX.Y.Z-custom.N` 或 Digest，
不得使用浮动 `latest`。公开 Release 默认匿名访问；GHCR 可见性需要在 Package
设置中单独核对。

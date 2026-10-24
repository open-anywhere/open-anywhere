# open-anywhere

**Open Anywhere：说出你的需要，让应用在身边的屏幕上诞生。**

[![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![codecov](https://codecov.io/gh/open-anywhere/open-anywhere/graph/badge.svg?token=U6SNW7NYZQ)](https://codecov.io/gh/open-anywhere/open-anywhere)

基于 Go 语言开发。

运行独立投屏服务：

```sh
go run ./cmd/casting -addr :8080 -interface en0
```

接口、目录组织和测试说明见 [Casting 服务](docs/casting.md)。

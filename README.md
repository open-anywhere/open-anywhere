<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo/open-anywhere-logo-dark.svg">
    <img src="assets/logo/open-anywhere-logo.svg" alt="open anywhere" width="440">
  </picture>
</p>

<p align="center">
  说出你的需要，让应用在身边的屏幕上诞生。
</p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white" alt="Go"></a>
  <a href="https://codecov.io/gh/open-anywhere/open-anywhere"><img src="https://codecov.io/gh/open-anywhere/open-anywhere/graph/badge.svg?token=U6SNW7NYZQ" alt="codecov"></a>
</p>

---

用语音描述需求，让云端编码 Agent 生成可运行的 Web 应用，并通过 AirPlay 在身边的屏幕上呈现和控制。

## 开发

基于 Go 语言开发。

```bash
go build ./...
go test ./... -race
go vet ./...
```

运行独立投屏服务：

```sh
go run ./cmd/casting -addr :8080 -interface en0
```

接口、目录组织和测试说明见 [Casting 服务](docs/casting.md)。

# NeoEdgeX App SDK v4

NeoEdgeX App SDK v4 is the public Go SDK for building NeoEdgeX node applications.

- Module path: `github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go`
- Third-party app packages:
  - `github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex`
  - `github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex/mock`

For normal app development, use the `neoedgex` package as the public SDK surface. Types such as `Node` and `Message` are available there directly.

Start with the external developer guides:

- [Developer Guide (English)](./docs/developer-guide.en.md)
- [第三方開發指南（繁體中文）](./docs/developer-guide.zh-tw.md)

Install:

```bash
go get github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go
```

For internal architecture and implementation notes, see [DESIGN.md](./DESIGN.md).
